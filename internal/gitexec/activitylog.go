package gitexec

import (
	"encoding/json"
	"io"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

type ActivityLog struct {
	mu sync.Mutex
	w  io.Writer
	c  io.Closer
}

func NewActivityLog(w io.Writer) *ActivityLog { return &ActivityLog{w: w} }

func OpenActivityLog(path string) (*ActivityLog, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return nil, err
	}
	f, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	return &ActivityLog{w: f, c: f}, nil
}

type logEntry struct {
	Time string   `json:"time"`
	Dir  string   `json:"dir"`
	Args []string `json:"args"`
	Ms   int64    `json:"ms"`
	Exit int      `json:"exit"`
}

func (l *ActivityLog) Record(dir string, args []string, d time.Duration, exit int) {
	if l == nil {
		return
	}
	red := make([]string, len(args))
	for i, a := range args {
		red[i] = RedactURL(a)
	}
	b, _ := json.Marshal(logEntry{Time: time.Now().Format(time.RFC3339), Dir: dir, Args: red, Ms: d.Milliseconds(), Exit: exit})
	l.mu.Lock()
	defer l.mu.Unlock()
	l.w.Write(append(b, '\n'))
}

func (l *ActivityLog) Close() error {
	if l == nil || l.c == nil {
		return nil
	}
	return l.c.Close()
}

// RedactURL rimuove user:password da un argomento che contiene un URL.
func RedactURL(s string) string {
	if !strings.Contains(s, "://") || !strings.Contains(s, "@") {
		return s
	}
	u, err := url.Parse(s)
	if err != nil || u.User == nil {
		return s
	}
	u.User = nil
	return u.String()
}
