// Package diff parses and builds git unified-diff patches.
//
// Parse expects the default git output (core.quotePath on): paths with special
// characters appear as C-style quoted strings ("a/caf\303\251.txt"), which are
// unquoted here. Output produced with -z is not supported. Paths must use the
// a/ and b/ prefixes: callers pass --src-prefix=a/ --dst-prefix=b/ so user
// settings like diff.noprefix or diff.mnemonicPrefix don't change them.
package diff

import (
	"fmt"
	"regexp"
	"strconv"
	"strings"
)

// LineKind is the one-character prefix of a hunk body line.
type LineKind string

const (
	Context   LineKind = " "
	Add       LineKind = "+"
	Del       LineKind = "-"
	NoNewline LineKind = "\\"
)

// Line is a single hunk body line. OldNo/NewNo are 1-based and 0 when absent.
// Text excludes the kind prefix and keeps any trailing "\r".
type Line struct {
	Kind  LineKind `json:"kind"`
	OldNo int      `json:"oldNo"`
	NewNo int      `json:"newNo"`
	Text  string   `json:"text"`
}

// Hunk is one "@@ -a,b +c,d @@ section" block. An omitted count in the header
// means 1.
type Hunk struct {
	OldStart int    `json:"oldStart"`
	OldLines int    `json:"oldLines"`
	NewStart int    `json:"newStart"`
	NewLines int    `json:"newLines"`
	Section  string `json:"section"`
	Lines    []Line `json:"lines"`
}

// FileDiff is the diff of one file. Status is one of A M D R C. For deleted
// files NewPath keeps the path from the "diff --git" header.
type FileDiff struct {
	OldPath    string `json:"oldPath"`
	NewPath    string `json:"newPath"`
	Status     string `json:"status"`
	Binary     bool   `json:"binary"`
	OldMode    string `json:"oldMode"`
	NewMode    string `json:"newMode"`
	Similarity int    `json:"similarity"`
	Hunks      []Hunk `json:"hunks"`
}

var hunkRE = regexp.MustCompile(`^@@ -(\d+)(?:,(\d+))? \+(\d+)(?:,(\d+))? @@ ?(.*)$`)

// Parse parses git unified diff output. Empty input yields no files and no error.
func Parse(data []byte) ([]FileDiff, error) {
	text := strings.TrimSuffix(string(data), "\n")
	if text == "" {
		return []FileDiff{}, nil
	}
	files := []FileDiff{}
	var (
		cur              *FileDiff
		hunk             *Hunk
		oldLeft, newLeft int
		oldNo, newNo     int
		combined         bool
	)
	for _, ln := range strings.Split(text, "\n") {
		if hunk != nil {
			if oldLeft > 0 || newLeft > 0 {
				k := byte(' ')
				if ln != "" {
					k = ln[0]
				}
				body := ""
				if len(ln) > 0 {
					body = ln[1:]
				}
				switch k {
				case ' ':
					hunk.Lines = append(hunk.Lines, Line{Kind: Context, OldNo: oldNo, NewNo: newNo, Text: body})
					oldNo, newNo, oldLeft, newLeft = oldNo+1, newNo+1, oldLeft-1, newLeft-1
				case '-':
					hunk.Lines = append(hunk.Lines, Line{Kind: Del, OldNo: oldNo, Text: body})
					oldNo, oldLeft = oldNo+1, oldLeft-1
				case '+':
					hunk.Lines = append(hunk.Lines, Line{Kind: Add, NewNo: newNo, Text: body})
					newNo, newLeft = newNo+1, newLeft-1
				case '\\':
					hunk.Lines = append(hunk.Lines, Line{Kind: NoNewline, Text: body})
				default:
					return nil, fmt.Errorf("diff: unexpected hunk line %q", ln)
				}
				continue
			}
			if strings.HasPrefix(ln, "\\") {
				hunk.Lines = append(hunk.Lines, Line{Kind: NoNewline, Text: ln[1:]})
				continue
			}
			hunk = nil
		}
		if combined && !strings.HasPrefix(ln, "diff ") {
			continue
		}
		combined = false
		switch {
		case strings.HasPrefix(ln, "diff --cc "), strings.HasPrefix(ln, "diff --combined "):
			// Combined diffs (unmerged paths) can't be staged hunk by hunk: report the
			// path as conflicted ("U") without hunks and skip its body.
			_, path, _ := strings.Cut(ln[len("diff --"):], " ")
			path = unquote(path)
			files = append(files, FileDiff{OldPath: path, NewPath: path, Status: "U", Hunks: []Hunk{}})
			cur, combined = nil, true
		case strings.HasPrefix(ln, "diff --git "):
			files = append(files, FileDiff{Status: "M", Hunks: []Hunk{}})
			cur = &files[len(files)-1]
			cur.OldPath, cur.NewPath = parseGitHeader(ln[len("diff --git "):])
		case cur == nil:
			// Ignore anything before the first file header.
		case strings.HasPrefix(ln, "@@ "):
			m := hunkRE.FindStringSubmatch(ln)
			if m == nil {
				return nil, fmt.Errorf("diff: invalid hunk header %q", ln)
			}
			h := Hunk{OldStart: atoi(m[1]), OldLines: atoiDef(m[2]), NewStart: atoi(m[3]), NewLines: atoiDef(m[4]), Section: m[5]}
			cur.Hunks = append(cur.Hunks, h)
			hunk = &cur.Hunks[len(cur.Hunks)-1]
			oldLeft, newLeft, oldNo, newNo = h.OldLines, h.NewLines, h.OldStart, h.NewStart
		case strings.HasPrefix(ln, "new file mode "):
			cur.Status, cur.NewMode = "A", strings.TrimPrefix(ln, "new file mode ")
		case strings.HasPrefix(ln, "deleted file mode "):
			cur.Status, cur.OldMode = "D", strings.TrimPrefix(ln, "deleted file mode ")
		case strings.HasPrefix(ln, "old mode "):
			cur.OldMode = strings.TrimPrefix(ln, "old mode ")
		case strings.HasPrefix(ln, "new mode "):
			cur.NewMode = strings.TrimPrefix(ln, "new mode ")
		case strings.HasPrefix(ln, "similarity index "):
			cur.Similarity = atoi(strings.TrimSuffix(strings.TrimPrefix(ln, "similarity index "), "%"))
		case strings.HasPrefix(ln, "rename from "):
			cur.Status, cur.OldPath = "R", unquote(strings.TrimPrefix(ln, "rename from "))
		case strings.HasPrefix(ln, "rename to "):
			cur.NewPath = unquote(strings.TrimPrefix(ln, "rename to "))
		case strings.HasPrefix(ln, "copy from "):
			cur.Status, cur.OldPath = "C", unquote(strings.TrimPrefix(ln, "copy from "))
		case strings.HasPrefix(ln, "copy to "):
			cur.NewPath = unquote(strings.TrimPrefix(ln, "copy to "))
		case strings.HasPrefix(ln, "--- "):
			// git appends a tab to unquoted names containing spaces.
			if p := strings.TrimSuffix(strings.TrimPrefix(ln, "--- "), "\t"); p != "/dev/null" {
				cur.OldPath = strings.TrimPrefix(unquote(p), "a/")
			}
		case strings.HasPrefix(ln, "+++ "):
			if p := strings.TrimSuffix(strings.TrimPrefix(ln, "+++ "), "\t"); p != "/dev/null" {
				cur.NewPath = strings.TrimPrefix(unquote(p), "b/")
			}
		case strings.HasPrefix(ln, "Binary files "), ln == "GIT binary patch":
			cur.Binary = true
		}
	}
	return files, nil
}

// parseGitHeader extracts the old and new paths from the part of a
// "diff --git" line after the prefix. Rename/copy and ---/+++ lines refine
// the result later.
func parseGitHeader(s string) (string, string) {
	if strings.HasPrefix(s, `"`) {
		if end := closingQuote(s); end > 0 {
			a := unquote(s[:end+1])
			b := unquote(strings.TrimSpace(s[end+1:]))
			return strings.TrimPrefix(a, "a/"), strings.TrimPrefix(b, "b/")
		}
	}
	// Unchanged name: "a/X b/X" is unambiguous even if X contains " b/".
	if (len(s)-5)%2 == 0 && len(s) > 5 {
		n := (len(s) - 5) / 2
		if s[:2] == "a/" && s[2+n:2+n+3] == " b/" && s[2:2+n] == s[5+n:] {
			return s[2 : 2+n], s[2 : 2+n]
		}
	}
	// Only the new name is quoted.
	if i := strings.Index(s, ` "b/`); i >= 0 {
		return strings.TrimPrefix(s[:i], "a/"), strings.TrimPrefix(unquote(s[i+1:]), "b/")
	}
	a, b, _ := strings.Cut(s, " b/")
	return strings.TrimPrefix(a, "a/"), b
}

func closingQuote(s string) int {
	for i := 1; i < len(s); i++ {
		switch s[i] {
		case '\\':
			i++
		case '"':
			return i
		}
	}
	return -1
}

func unquote(s string) string {
	if len(s) >= 2 && s[0] == '"' {
		if u, err := strconv.Unquote(s); err == nil {
			return u
		}
	}
	return s
}

func atoi(s string) int { n, _ := strconv.Atoi(s); return n }

func atoiDef(s string) int {
	if s == "" {
		return 1
	}
	return atoi(s)
}
