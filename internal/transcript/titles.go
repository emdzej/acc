package transcript

import (
	"bufio"
	"bytes"
	"encoding/json"
	"io"
	"os"
)

// Titles returns the session's latest /rename title and auto-generated title.
// Claude Code re-appends these records often, so the tail usually has them;
// full=true scans the whole file (used once per session).
func Titles(path string, full bool) (custom, ai string, err error) {
	real, err := checkPath(path)
	if err != nil {
		return "", "", err
	}
	f, err := os.Open(real)
	if err != nil {
		return "", "", err
	}
	defer f.Close()
	if !full {
		if fi, err := f.Stat(); err == nil && fi.Size() > tailBytes {
			f.Seek(fi.Size()-tailBytes, io.SeekStart)
		}
	}
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 256<<10), 16<<20)
	for sc.Scan() {
		b := sc.Bytes()
		// Cheap pre-filter: title records are rare among large message lines.
		if !bytes.Contains(b, []byte(`-title"`)) {
			continue
		}
		var l struct {
			Type        string `json:"type"`
			CustomTitle string `json:"customTitle"`
			AITitle     string `json:"aiTitle"`
		}
		if json.Unmarshal(b, &l) != nil {
			continue // e.g. the partial first line of the tail window
		}
		switch l.Type {
		case "custom-title":
			custom = l.CustomTitle
		case "ai-title":
			ai = l.AITitle
		}
	}
	return custom, ai, sc.Err()
}
