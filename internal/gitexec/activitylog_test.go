package gitexec

import (
	"bytes"
	"encoding/json"
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
