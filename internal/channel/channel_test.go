package channel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"
	"testing"
	"time"
)

func TestOptedIn(t *testing.T) {
	cases := []struct {
		args string
		want bool
	}{
		{"claude --dangerously-load-development-channels server:acc", true},
		{"claude --dangerously-load-development-channels server:webhook server:acc --resume x", true},
		{"claude --channels=server:acc", true},
		{"claude --dangerously-load-development-channels plugin:x@y,server:acc", true},
		{"claude --resume server:acc", false},
		{"claude", false},
		{"claude --dangerously-load-development-channels server:acc -p hi", false},
		{"claude --dangerously-load-development-channels server:accx", false},
	}
	for _, c := range cases {
		if got := optedIn(strings.Fields(c.args)); got != c.want {
			t.Errorf("%q: got %v", c.args, got)
		}
	}
}

func TestInitializeDeclaresChannel(t *testing.T) {
	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	go Serve(context.Background(), inR, outW, io.Discard, "test")
	go io.WriteString(inW, `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2099-01-01"}}`+"\n"+
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`+"\n")

	sc := bufio.NewScanner(outR)
	read := func() map[string]any {
		done := make(chan bool)
		go func() { sc.Scan(); done <- true }()
		select {
		case <-done:
		case <-time.After(2 * time.Second):
			t.Fatal("no response")
		}
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatal(err)
		}
		return m
	}
	r := read()["result"].(map[string]any)
	if r["protocolVersion"] != latestVersion {
		t.Errorf("protocolVersion %v", r["protocolVersion"])
	}
	exp := r["capabilities"].(map[string]any)["experimental"].(map[string]any)
	if _, ok := exp["claude/channel"]; !ok {
		t.Error("claude/channel capability missing")
	}
	if e := read()["error"]; e == nil {
		t.Error("unknown method should error")
	}
}
