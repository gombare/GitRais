//go:build !darwin

package watch

import (
	"errors"
	"runtime"
)

// NewSource is only implemented on macOS (FSEvents).
func NewSource(root string) (Source, error) {
	return nil, errors.New("watch: file system events are not supported on " + runtime.GOOS)
}
