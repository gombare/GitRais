package diff

import (
	"fmt"
	"strings"
	"unicode/utf8"
)

// Selection maps a hunk index to the set of selected line indices (Add/Del
// lines within that hunk).
type Selection map[int]map[int]bool

// Mode says which way the built patch will be applied.
type Mode int

const (
	// ForStage builds a patch from an index->worktree diff, applied with
	// "git apply --cached".
	ForStage Mode = iota
	// ForUnstage builds a patch from a HEAD->index diff, applied with
	// "git apply --cached --reverse".
	ForUnstage
)

// SelectHunk selects every changed line of one hunk.
func SelectHunk(f FileDiff, hunk int) Selection {
	s := Selection{hunk: {}}
	for i, l := range f.Hunks[hunk].Lines {
		if l.Kind == Add || l.Kind == Del {
			s[hunk][i] = true
		}
	}
	return s
}

// quotePath renders a path the way git does in patch headers: plain unless it
// contains characters git would quote (quote, backslash, control characters).
// Non-ASCII UTF-8 is emitted raw, which git apply accepts.
func quotePath(p string) string {
	needs := !utf8.ValidString(p)
	for _, r := range p {
		if r < 0x20 || r == 0x7f || r == '"' || r == '\\' {
			needs = true
		}
	}
	if !needs {
		return p
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(p); i++ {
		c := p[i]
		switch {
		case c == '"' || c == '\\':
			b.WriteByte('\\')
			b.WriteByte(c)
		case c == '\t':
			b.WriteString(`\t`)
		case c == '\n':
			b.WriteString(`\n`)
		case c == '\r':
			b.WriteString(`\r`)
		case c < 0x20 || c == 0x7f:
			b.WriteString(`\` + fmt.Sprintf("%03o", c))
		default:
			b.WriteByte(c)
		}
	}
	b.WriteByte('"')
	return b.String()
}

// hdrName adds the a/ or b/ prefix and quoting; git appends a tab after
// unquoted names containing spaces in ---/+++ lines.
func hdrName(prefix, p string, fileLine bool) string {
	q := quotePath(prefix + p)
	if fileLine && !strings.HasPrefix(q, `"`) && strings.Contains(q, " ") {
		q += "\t"
	}
	return q
}

// BuildPatch builds a single-file patch containing only the selected changes.
//
// ForStage (index->worktree diff): unselected Add lines are dropped and
// unselected Del lines become context. ForUnstage (HEAD->index diff, applied
// in reverse): unselected Add lines become context and unselected Del lines
// are dropped. Hunk headers are recomputed. A "\ No newline" marker is kept
// only when the line it refers to is emitted. It returns false when nothing is
// selected, or when the file is not a plain modification (binary, added,
// deleted, renamed, conflicted): partial staging is only supported for those
// with Status "M".
func BuildPatch(f FileDiff, sel Selection, mode Mode) ([]byte, bool) {
	if f.Binary || f.Status != "M" {
		return nil, false
	}
	oldP, newP := f.OldPath, f.NewPath
	if oldP == "" {
		oldP = newP
	}
	var b strings.Builder
	fmt.Fprintf(&b, "diff --git %s %s\n--- %s\n+++ %s\n",
		hdrName("a/", oldP, false), hdrName("b/", newP, false),
		hdrName("a/", oldP, true), hdrName("b/", newP, true))
	offset, any := 0, false
	for hi, h := range f.Hunks {
		picked := sel[hi]
		if len(picked) == 0 {
			continue
		}
		var body []string
		oldN, newN, changes := 0, 0, 0
		emitted := false
		for li, l := range h.Lines {
			chosen := picked[li]
			switch l.Kind {
			case Context:
				body = append(body, " "+l.Text)
				oldN, newN, emitted = oldN+1, newN+1, true
			case Add:
				switch {
				case chosen:
					body = append(body, "+"+l.Text)
					newN, changes, emitted = newN+1, changes+1, true
				case mode == ForUnstage:
					body = append(body, " "+l.Text)
					oldN, newN, emitted = oldN+1, newN+1, true
				default:
					emitted = false
				}
			case Del:
				switch {
				case chosen:
					body = append(body, "-"+l.Text)
					oldN, changes, emitted = oldN+1, changes+1, true
				case mode == ForStage:
					body = append(body, " "+l.Text)
					oldN, newN, emitted = oldN+1, newN+1, true
				default:
					emitted = false
				}
			case NoNewline:
				if emitted {
					body = append(body, "\\"+l.Text)
				}
			}
		}
		if changes == 0 {
			continue
		}
		any = true
		newStart := h.OldStart + offset
		if mode == ForUnstage {
			// The patch is applied in reverse onto the index, which is the
			// "new" side of the HEAD->index diff: keep its original position.
			newStart = h.NewStart
		}
		fmt.Fprintf(&b, "@@ -%d,%d +%d,%d @@\n", h.OldStart, oldN, newStart, newN)
		for _, l := range body {
			b.WriteString(l)
			b.WriteByte('\n')
		}
		offset += newN - oldN
	}
	if !any {
		return nil, false
	}
	return []byte(b.String()), true
}
