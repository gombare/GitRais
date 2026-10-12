package graph

import (
	"context"
	"math/rand"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"runtime/debug"
	"slices"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"gitrais/internal/genrepo"
	"gitrais/internal/gitexec"
	"gitrais/internal/testutil"
)

// Performance targets on a 1M-commit repository.
const (
	targetFirstBatch = time.Second
	targetGetRows    = 5 * time.Millisecond
	targetMemMB      = 300
)

// TestPerfBigRepo measures the loader on a large repository. It runs only when
// GITRAIS_BIGREPO names an existing repository (`make bigrepo`) or GITRAIS_PERF=1,
// in which case it generates GITRAIS_PERF_N commits (default 1M) in a temp dir.
func TestPerfBigRepo(t *testing.T) {
	dir := os.Getenv("GITRAIS_BIGREPO")
	if testing.Short() || (dir == "" && os.Getenv("GITRAIS_PERF") != "1") {
		t.Skip("set GITRAIS_PERF=1 or GITRAIS_BIGREPO (make bench)")
	}
	r := testutil.Runner(t)
	if dir == "" {
		n := 1_000_000
		if v := os.Getenv("GITRAIS_PERF_N"); v != "" {
			var err error
			if n, err = strconv.Atoi(v); err != nil {
				t.Fatal(err)
			}
		}
		dir = filepath.Join(t.TempDir(), "big")
		t0 := time.Now()
		if err := genrepo.Create(context.Background(), r, dir, n, 12); err != nil {
			t.Fatal(err)
		}
		t.Logf("generated %d commits in %v, repo size %s", n, time.Since(t0).Round(time.Second), diskUsage(t, dir))
	}

	// Initial load.
	runtime.GC()
	s := startSampler()
	idx, first, total := perfLoad(t, r, dir, nil)
	peakHeap, peakRSS := s.stop()
	heap, rss, foot := settledMem()
	t.Logf("load: rows=%d first batch=%v total=%v", idx.Len(), first.Round(time.Millisecond), total.Round(time.Millisecond))
	t.Logf("load: heap after GC=%.0fMB RSS settled=%.0fMB footprint settled=%.0fMB; during load peak heap=%.0fMB peak RSS=%.0fMB",
		heap, rss, foot, peakHeap, peakRSS)

	// Random 100-row windows, as the frontend scrolls.
	rng := rand.New(rand.NewSource(1))
	const calls = 2000
	durs := make([]time.Duration, calls)
	for i := range durs {
		start := rng.Intn(idx.Len() - 100)
		t0 := time.Now()
		rows := idx.Rows(start, 100)
		durs[i] = time.Since(t0)
		if len(rows) != 100 {
			t.Fatalf("Rows(%d,100) returned %d rows", start, len(rows))
		}
	}
	slices.Sort(durs)
	p50, p99 := durs[calls/2], durs[calls*99/100]
	t.Logf("GetRows(100): p50=%v p99=%v max=%v", p50, p99, durs[calls-1])
	t0 := time.Now()
	idx.Rows(idx.Len()/2+checkpointEvery-1, 10_000) // the largest window App.Rows allows, worst offset
	t.Logf("GetRows(10000): %v", time.Since(t0))

	// Find probes the chunks from the last one: the first row is the worst case.
	h := idx.Rows(0, 1)[0].Hash
	t0 = time.Now()
	if i, ok := idx.Find(h); !ok || i != 0 {
		t.Fatalf("Find(first) = %d %v", i, ok)
	}
	tFind := time.Since(t0)
	t0 = time.Now()
	if _, ok := idx.Find(strings.Repeat("0", len(h))); ok {
		t.Fatal("Find(missing) found")
	}
	t.Logf("Find: first row %v, missing %v", tFind, time.Since(t0))

	// Reload as Session.Reload does it: the old index is dropped when the new
	// one publishes its first batch.
	old := idx
	idx = nil
	s = startSampler()
	idx2, _, total2 := perfLoad(t, r, dir, func() { old = nil })
	peakHeap2, peakRSS2 := s.stop()
	t.Logf("reload (swap at first batch): total=%v peak heap=%.0fMB peak RSS=%.0fMB (process max)",
		total2.Round(time.Millisecond), peakHeap2, peakRSS2)
	runtime.KeepAlive(old)

	// Worst case: the old index stays alive until the new one is complete
	// (e.g. a reload that fails before its first batch keeps the old one).
	s = startSampler()
	idx3, _, _ := perfLoad(t, r, dir, nil)
	peakHeap3, peakRSS3 := s.stop()
	heap3, rss3, foot3 := settledMem()
	t.Logf("reload (old kept to the end): old+new heap after GC=%.0fMB RSS settled=%.0fMB footprint settled=%.0fMB; peak heap=%.0fMB peak RSS=%.0fMB",
		heap3, rss3, foot3, peakHeap3, peakRSS3)
	runtime.KeepAlive(idx2)
	runtime.KeepAlive(idx3)

	if first > targetFirstBatch {
		t.Errorf("first batch %v > %v", first, targetFirstBatch)
	}
	if p99 > targetGetRows {
		t.Errorf("GetRows p99 %v > %v", p99, targetGetRows)
	}
	if heap > targetMemMB {
		t.Errorf("heap %.0fMB > %dMB", heap, targetMemMB)
	}
}

// perfLoad loads dir into a new index; onFirst (if not nil) runs at the first batch.
func perfLoad(t *testing.T, r *gitexec.Runner, dir string, onFirst func()) (idx *Index, first, total time.Duration) {
	t.Helper()
	idx = NewIndex()
	start := time.Now()
	err := Load(context.Background(), r, dir, idx, LoadOptions{OnProgress: func(int) {
		if first == 0 {
			first = time.Since(start)
			if onFirst != nil {
				onFirst()
			}
		}
	}})
	if err != nil {
		t.Fatal(err)
	}
	return idx, first, time.Since(start)
}

// sampler records the peak heap in use and the peak resident set size.
type sampler struct {
	done     chan struct{}
	wg       sync.WaitGroup
	peakHeap uint64
}

func startSampler() *sampler {
	s := &sampler{done: make(chan struct{})}
	s.wg.Add(1)
	go func() {
		defer s.wg.Done()
		tick := time.NewTicker(20 * time.Millisecond)
		defer tick.Stop()
		var m runtime.MemStats
		for {
			runtime.ReadMemStats(&m)
			s.peakHeap = max(s.peakHeap, m.HeapInuse)
			select {
			case <-s.done:
				return
			case <-tick.C:
			}
		}
	}()
	return s
}

// stop returns the peak heap in use and the process peak RSS so far (MB).
func (s *sampler) stop() (heapMB, rssMB float64) {
	close(s.done)
	s.wg.Wait()
	var ru syscall.Rusage
	_ = syscall.Getrusage(syscall.RUSAGE_SELF, &ru)
	return float64(s.peakHeap) / 1e6, float64(ru.Maxrss) / 1e6 // Maxrss is in bytes on macOS
}

// settledMem returns the heap in use after a GC, and the RSS and the physical
// footprint (what Activity Monitor shows as Memory) after returning free memory
// to the OS (MB). On macOS RSS keeps counting pages released with
// MADV_FREE_REUSABLE until the kernel reclaims them; the footprint does not.
func settledMem() (heapMB, rssMB, footMB float64) {
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	debug.FreeOSMemory()
	time.Sleep(time.Second) // let the OS account for the released pages
	out, err := exec.Command("ps", "-o", "rss=", "-p", strconv.Itoa(os.Getpid())).Output()
	if err == nil {
		kb, _ := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
		rssMB = kb * 1024 / 1e6
	}
	return float64(m.HeapInuse) / 1e6, rssMB, footprintMB()
}

// footprintMB returns the physical footprint of this process (macOS), or 0.
func footprintMB() float64 {
	out, err := exec.Command("footprint", strconv.Itoa(os.Getpid())).Output()
	if err != nil {
		return 0
	}
	_, rest, ok := strings.Cut(string(out), "Footprint: ")
	if !ok {
		return 0
	}
	f := strings.Fields(rest)
	if len(f) < 2 {
		return 0
	}
	v, _ := strconv.ParseFloat(f[0], 64)
	switch f[1] {
	case "KB":
		v /= 1e3
	case "GB":
		v *= 1e3
	case "B":
		v /= 1e6
	}
	return v * 1.048576 // footprint uses binary units
}

func diskUsage(t *testing.T, dir string) string {
	out, err := exec.Command("du", "-sh", dir).Output()
	if err != nil {
		return "?"
	}
	return strings.Fields(string(out))[0]
}

// benchRepo generates a 20k-commit repository for the benchmarks.
func benchRepo(b *testing.B) (*gitexec.Runner, string) {
	b.Helper()
	r := testutil.Runner(b)
	dir := filepath.Join(b.TempDir(), "repo")
	if err := genrepo.Create(context.Background(), r, dir, 20_000, 12); err != nil {
		b.Fatal(err)
	}
	return r, dir
}

func BenchmarkLoad20k(b *testing.B) {
	r, dir := benchRepo(b)
	b.ResetTimer()
	for b.Loop() {
		if err := Load(context.Background(), r, dir, NewIndex(), LoadOptions{}); err != nil {
			b.Fatal(err)
		}
	}
}

// BenchmarkRows100 measures a random 100-row window, segment replay included.
func BenchmarkRows100(b *testing.B) {
	r, dir := benchRepo(b)
	idx := NewIndex()
	if err := Load(context.Background(), r, dir, idx, LoadOptions{}); err != nil {
		b.Fatal(err)
	}
	rng := rand.New(rand.NewSource(1))
	b.ResetTimer()
	for b.Loop() {
		idx.Rows(rng.Intn(idx.Len()-100), 100)
	}
}
