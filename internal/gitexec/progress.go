package gitexec

import (
	"bytes"
	"regexp"
	"strconv"
)

type Progress struct {
	Phase   string `json:"phase"`
	Percent int    `json:"percent"`
	Current int    `json:"current"`
	Total   int    `json:"total"`
}

var progressRE = regexp.MustCompile(`^(?:remote: )?([A-Za-z][A-Za-z ]*?):\s+(\d+)% \((\d+)/(\d+)\)`)

func ParseProgressLine(line string) (Progress, bool) {
	m := progressRE.FindStringSubmatch(line)
	if m == nil {
		return Progress{}, false
	}
	pct, _ := strconv.Atoi(m[2])
	cur, _ := strconv.Atoi(m[3])
	tot, _ := strconv.Atoi(m[4])
	return Progress{Phase: m[1], Percent: pct, Current: cur, Total: tot}, true
}

// ProgressParser receives git's stderr (lines separated by \r or \n) and calls On for each progress line.
type ProgressParser struct {
	On  func(Progress)
	buf []byte
}

func (p *ProgressParser) Write(b []byte) {
	p.buf = append(p.buf, b...)
	for {
		i := bytes.IndexAny(p.buf, "\r\n")
		if i < 0 {
			return
		}
		line := string(p.buf[:i])
		p.buf = p.buf[i+1:]
		if pr, ok := ParseProgressLine(line); ok && p.On != nil {
			p.On(pr)
		}
	}
}
