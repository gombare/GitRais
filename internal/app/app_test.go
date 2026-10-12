package app

import "testing"

func TestVersion(t *testing.T) {
	if got := New().Version(); got != "0.1.0" {
		t.Fatalf("Version() = %q, want 0.1.0", got)
	}
}
