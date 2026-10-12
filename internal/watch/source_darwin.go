//go:build darwin

package watch

import (
	"strings"
	"sync"
	"time"

	"github.com/fsnotify/fsevents"
)

type fseSource struct {
	es        *fsevents.EventStream
	out       chan []string
	done      chan struct{}
	finished  chan struct{}
	closeOnce sync.Once
}

// NewSource watches root recursively with FSEvents. root must be a real path
// (filepath.EvalSymlinks): FSEvents reports resolved paths.
func NewSource(root string) (Source, error) {
	es := &fsevents.EventStream{
		Paths:   []string{root},
		Latency: 50 * time.Millisecond,
		Flags:   fsevents.FileEvents | fsevents.NoDefer,
		// Buffered so the C callback rarely blocks while we are busy or stopping.
		Events: make(chan []fsevents.Event, 64),
	}
	if err := es.Start(); err != nil {
		return nil, err
	}
	s := &fseSource{es: es, out: make(chan []string, 16), done: make(chan struct{}), finished: make(chan struct{})}
	go s.loop()
	return s, nil
}

func (s *fseSource) loop() {
	defer close(s.finished)
	defer close(s.out)
	defer s.stop()
	for {
		select {
		case <-s.done:
			return
		case msg, ok := <-s.es.Events:
			if !ok {
				return
			}
			paths := make([]string, 0, len(msg))
			for _, e := range msg {
				p := e.Path
				if !strings.HasPrefix(p, "/") {
					p = "/" + p
				}
				paths = append(paths, p)
			}
			select {
			case s.out <- paths:
			case <-s.done:
				return
			}
		}
	}
}

// stop halts the stream while draining Events: the FSEvents callback does a
// blocking send and would otherwise wedge Stop or leak its thread.
func (s *fseSource) stop() {
	stopped := make(chan struct{})
	go func() {
		for {
			select {
			case <-s.es.Events:
			case <-stopped:
				return
			}
		}
	}()
	s.es.Stop()
	close(stopped)
}

func (s *fseSource) Events() <-chan []string { return s.out }

// Close stops the stream and waits until its goroutine has exited. Idempotent.
func (s *fseSource) Close() error {
	s.closeOnce.Do(func() { close(s.done) })
	<-s.finished
	return nil
}
