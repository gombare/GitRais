package gitexec

import "testing"

// SetMaxRecordForTest lowers the Stream record size limit for the duration of the test.
func SetMaxRecordForTest(t testing.TB, n int) {
	old := maxRecord
	maxRecord = n
	t.Cleanup(func() { maxRecord = old })
}
