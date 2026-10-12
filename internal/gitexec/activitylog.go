package gitexec

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"regexp"
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

var userinfoRe = regexp.MustCompile(`([A-Za-z][A-Za-z0-9+.-]*://)[^\s]+@`)

// RedactURL rimuove la userinfo (user:password@) da ogni URL con schema presente nella stringa,
// anche se incorporato in un argomento più grande (--repo=URL) o in un messaggio di errore.
// Il match è volutamente avido (fino all'ultima @ prima di uno spazio) così password con
// '/', '#', '@' o '%' non lasciano residui. Le forme scp-like (git@host:path) non cambiano.
func RedactURL(s string) string {
	if !strings.Contains(s, "://") {
		return s
	}
	return userinfoRe.ReplaceAllString(s, "$1")
}
