package gitexec

import "testing"

// SetMaxRecordForTest riduce il limite di dimensione dei record di Stream per la durata del test.
func SetMaxRecordForTest(t testing.TB, n int) {
	old := maxRecord
	maxRecord = n
	t.Cleanup(func() { maxRecord = old })
}
