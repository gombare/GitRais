package main

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunFlags(t *testing.T) {
	for _, args := range [][]string{{"-n", "0"}, {"-branches", "0"}, {"-n", "x"}, {"-bogus"}, {"extra"}} {
		var out, errb bytes.Buffer
		if code := run(args, &out, &errb); code != 2 || out.Len() != 0 || errb.Len() == 0 {
			t.Errorf("run(%v) = %d, stdout %d bytes, stderr %q", args, code, out.Len(), errb.String())
		}
	}
	var out, errb bytes.Buffer
	if code := run([]string{"-n", "3", "-branches", "2"}, &out, &errb); code != 0 {
		t.Fatalf("exit %d: %s", code, errb.String())
	}
	s := "\n" + out.String()
	if got := strings.Count(s, "\ncommit refs/heads/"); got != 3 {
		t.Fatalf("%d commits in stream:\n%s", got, s)
	}
	if !strings.Contains(s, "\nreset refs/heads/main\n") {
		t.Fatalf("missing main reset:\n%s", s)
	}
}
