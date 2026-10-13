package gitexec

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestActivityLogRecord(t *testing.T) {
	var buf bytes.Buffer
	l := NewActivityLog(&buf)
	l.Record("/r", []string{"fetch", "https://user:secret@host/r.git"}, 1500*time.Millisecond, 0)
	var e struct {
		Dir  string   `json:"dir"`
		Args []string `json:"args"`
		Ms   int64    `json:"ms"`
		Exit int      `json:"exit"`
	}
	if err := json.Unmarshal(buf.Bytes(), &e); err != nil {
		t.Fatal(err)
	}
	if e.Dir != "/r" || e.Args[1] != "https://host/r.git" || e.Ms != 1500 || e.Exit != 0 {
		t.Fatalf("entry %+v", e)
	}
	if bytes.Contains(buf.Bytes(), []byte("secret")) {
		t.Fatal("credentials leaked into log")
	}
}

func TestActivityLogNilSafe(t *testing.T) {
	var l *ActivityLog
	l.Record("/r", []string{"status"}, 0, 0)
}

func TestRedactURL(t *testing.T) {
	cases := []struct{ in, want string }{
		{"https://u:p#x@h/r.git", "https://h/r.git"},
		{"https://u:a/b@h/r.git", "https://h/r.git"},
		{"https://u:p%zz@h/r", "https://h/r"},
		{"--repo=https://u:p@h/r", "--repo=https://h/r"},
		{"remote.origin.url=https://u:p@h/r", "remote.origin.url=https://h/r"},
		{"https://h/r", "https://h/r"},
		{"git@github.com:o/r.git", "git@github.com:o/r.git"},
		{"ssh://git@h/r", "ssh://h/r"},
		{"fatal: unable to access 'https://u:p@h/r/': error", "fatal: unable to access 'https://h/r/': error"},
	}
	for _, c := range cases {
		if got := RedactURL(c.in); got != c.want {
			t.Errorf("RedactURL(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

// A log larger than maxLogSize at open is rotated to <path>.1 (replacing an
// older one); a smaller one is appended to.
func TestOpenActivityLogRotates(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "activity.log")
	big := bytes.Repeat([]byte("x"), 5<<20+1)
	if err := os.WriteFile(p, big, 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p+".1", []byte("older"), 0o600); err != nil {
		t.Fatal(err)
	}
	l, err := OpenActivityLog(p)
	if err != nil {
		t.Fatal(err)
	}
	l.Record("/r", []string{"status"}, 0, 0)
	l.Close()
	if st, err := os.Stat(p + ".1"); err != nil || st.Size() != int64(len(big)) {
		t.Fatalf("rotated log: %v %v", st, err)
	}
	if b, _ := os.ReadFile(p); len(b) == 0 || len(b) > 1000 {
		t.Fatalf("new log size = %d", len(b))
	}
	if _, err := os.Stat(p + ".2"); err == nil {
		t.Fatal("only one rotated log must be kept")
	}

	// Small: appended, not rotated.
	l, _ = OpenActivityLog(p)
	l.Record("/r", []string{"log"}, 0, 0)
	l.Close()
	if b, _ := os.ReadFile(p); strings.Count(string(b), "\n") != 2 {
		t.Fatalf("log = %q", b)
	}
	if st, _ := os.Stat(p + ".1"); st.Size() != int64(len(big)) {
		t.Fatal("small log rotated")
	}
}
