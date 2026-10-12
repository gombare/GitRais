package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func TestLoadDefaults(t *testing.T) {
	s := Open(filepath.Join(t.TempDir(), "x", "state.json"))
	st, err := s.Load()
	if err != nil {
		t.Fatal(err)
	}
	if st.Drafts == nil || st.Layout.LeftWidth != 260 || st.Layout.RightWidth != 380 || len(st.Tabs) != 0 {
		t.Fatalf("defaults = %+v", st)
	}
}

func TestUpdatePersists(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := Open(p)
	_, err := s.Update(func(st *State) {
		st.Tabs = append(st.Tabs, Tab{ID: "t1", Path: "/r"})
		st.ActiveTab = "t1"
		st.Drafts["/r"] = Draft{Summary: "wip"}
	})
	if err != nil {
		t.Fatal(err)
	}
	st, err := Open(p).Load()
	if err != nil || st.ActiveTab != "t1" || st.Drafts["/r"].Summary != "wip" {
		t.Fatalf("reload = %+v %v", st, err)
	}
}

func TestRecentsAndFavorites(t *testing.T) {
	var st State
	for i := 0; i < 25; i++ {
		AddRecent(&st, fmt.Sprintf("/r%d", i))
	}
	AddRecent(&st, "/r10")
	if len(st.Recents) != MaxRecents || st.Recents[0] != "/r10" || st.Recents[1] != "/r24" {
		t.Fatalf("recents = %v", st.Recents)
	}
	ToggleFavorite(&st, "/a")
	ToggleFavorite(&st, "/b")
	ToggleFavorite(&st, "/a")
	if len(st.Favorites) != 1 || st.Favorites[0] != "/b" {
		t.Fatalf("favorites = %v", st.Favorites)
	}
}

func TestNoNullsInJSON(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	// fn nils everything; the file must still contain no nulls.
	if _, err := Open(p).Update(func(st *State) { *st = State{} }); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	if strings.Contains(string(b), "null") {
		t.Fatalf("null in output: %s", b)
	}
	// A file written with nulls loads normalised.
	os.WriteFile(p, []byte(`{"tabs":null,"recents":null,"favorites":null,"drafts":null}`), 0o600)
	st, err := Open(p).Load()
	if err != nil || st.Tabs == nil || st.Recents == nil || st.Favorites == nil || st.Drafts == nil {
		t.Fatalf("not normalised: %+v %v", st, err)
	}
	if st.Layout.LeftWidth != 260 {
		t.Fatalf("layout default lost: %+v", st.Layout)
	}
}

func TestPermissionsAndNoTempLeftovers(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "app")
	p := filepath.Join(dir, "state.json")
	if _, err := Open(p).Update(func(st *State) { st.ActiveTab = "a" }); err != nil {
		t.Fatal(err)
	}
	di, _ := os.Stat(dir)
	fi, _ := os.Stat(p)
	if di.Mode().Perm() != 0o700 || fi.Mode().Perm() != 0o600 {
		t.Fatalf("modes dir=%v file=%v", di.Mode().Perm(), fi.Mode().Perm())
	}
	ents, _ := os.ReadDir(dir)
	if len(ents) != 1 {
		t.Fatalf("leftover files: %v", ents)
	}
}

func TestCorruptFileIsQuarantined(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte("{not json"), 0o600)
	s := Open(p)
	st, err := s.Load()
	if !errors.Is(err, ErrCorrupt) {
		t.Fatalf("err = %v, want ErrCorrupt", err)
	}
	if st.Drafts == nil || st.Layout.LeftWidth != 260 {
		t.Fatalf("default state expected: %+v", st)
	}
	b, err := os.ReadFile(p + ".corrupt")
	if err != nil || string(b) != "{not json" {
		t.Fatalf("corrupt copy = %q %v", b, err)
	}
	// The next Update starts from defaults and does not destroy the copy.
	if _, err := s.Update(func(st *State) { st.ActiveTab = "z" }); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p + ".corrupt"); string(b) != "{not json" {
		t.Fatalf("corrupt copy lost: %q", b)
	}
	if st, err := Open(p).Load(); err != nil || st.ActiveTab != "z" {
		t.Fatalf("reload = %+v %v", st, err)
	}
}

func TestUpdateOnCorruptKeepsFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	os.WriteFile(p, []byte(`[1,2]`), 0o600) // wrong shape
	if _, err := Open(p).Update(func(st *State) {}); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(p + ".corrupt"); err != nil || string(b) != `[1,2]` {
		t.Fatalf("corrupt copy = %q %v", b, err)
	}
}

func TestConcurrentUpdates(t *testing.T) {
	p := filepath.Join(t.TempDir(), "state.json")
	s := Open(p)
	var wg sync.WaitGroup
	for i := 0; i < 30; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := s.Update(func(st *State) { st.Tabs = append(st.Tabs, Tab{ID: "t"}) }); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	st, err := Open(p).Load()
	if err != nil || len(st.Tabs) != 30 {
		t.Fatalf("tabs=%d err=%v", len(st.Tabs), err)
	}
	var raw map[string]any
	b, _ := os.ReadFile(p)
	if json.Unmarshal(b, &raw) != nil {
		t.Fatal("invalid json on disk")
	}
}

func TestDefaultPathEnvOverride(t *testing.T) {
	t.Setenv("GITRAIS_STATE_PATH", "/tmp/custom/state.json")
	if p, err := DefaultPath(); err != nil || p != "/tmp/custom/state.json" {
		t.Fatalf("got %q %v", p, err)
	}
	t.Setenv("GITRAIS_STATE_PATH", "")
	t.Setenv("HOME", "/h")
	if p, _ := DefaultPath(); p != "/h/Library/Application Support/Gitrais/state.json" {
		t.Fatalf("got %q", p)
	}
}
