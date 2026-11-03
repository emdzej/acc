package hook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/model"
)

func TestSpoolReplay(t *testing.T) {
	t.Setenv("ACC_HOME", t.TempDir())
	// An event spooled by an older version, one file per event.
	os.MkdirAll(config.SpoolDir(), 0o700)
	old, _ := json.Marshal(model.Event{SessionID: "s", Name: "SessionStart", TS: 1})
	os.WriteFile(filepath.Join(config.SpoolDir(), "1-abcd.json"), old, 0o600)
	for i := 2; i <= 4; i++ {
		if err := spool(model.Event{SessionID: "s", Name: fmt.Sprint("E", i), TS: int64(i), Deliver: true}, errors.New("down")); err != nil {
			t.Fatal(err)
		}
	}

	// A failing consumer leaves what it didn't accept.
	var got []string
	n, err := ReplaySpool(func(ev model.Event) error {
		if ev.Name == "E3" {
			return errors.New("db busy")
		}
		if ev.Deliver {
			t.Error("spooled events must not request delivery")
		}
		got = append(got, ev.Name)
		return nil
	})
	if err == nil || n != 2 || fmt.Sprint(got) != "[SessionStart E2]" {
		t.Fatalf("first replay: %d %v %v", n, got, err)
	}
	spool(model.Event{SessionID: "s", Name: "E5", TS: 5}, errors.New("down"))
	got = nil
	if n, err := ReplaySpool(func(ev model.Event) error { got = append(got, ev.Name); return nil }); err != nil || n != 3 || fmt.Sprint(got) != "[E3 E4 E5]" {
		t.Fatalf("second replay: %d %v %v", n, got, err)
	}
	if left, _ := os.ReadDir(config.SpoolDir()); len(left) != 0 {
		t.Errorf("spool not emptied: %v", left)
	}
	if n, err := ReplaySpool(func(model.Event) error { return nil }); n != 0 || err != nil {
		t.Errorf("empty replay: %d %v", n, err)
	}
}

func TestSpoolConcurrent(t *testing.T) {
	t.Setenv("ACC_HOME", t.TempDir())
	var mu sync.Mutex
	seen := map[string]int{}
	take := func(ev model.Event) error { mu.Lock(); seen[ev.SessionID]++; mu.Unlock(); return nil }
	var wg sync.WaitGroup
	for i := range 40 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := spool(model.Event{SessionID: fmt.Sprint("s", i), Name: "Stop", Message: string(make([]byte, 8000))}, errors.New("down")); err != nil {
				t.Error(err)
			}
		}()
		if i == 20 { // a replay racing the writers
			wg.Add(1)
			go func() { defer wg.Done(); ReplaySpool(take) }()
		}
	}
	wg.Wait()
	ReplaySpool(take)
	// Every event replayed exactly once, across both replays.
	if len(seen) != 40 {
		t.Fatalf("replayed %d of 40 sessions", len(seen))
	}
	for id, c := range seen {
		if c != 1 {
			t.Errorf("%s replayed %d times", id, c)
		}
	}
}
