// Package store persists UI state (tabs, recents, favorites, drafts, layout)
// as a JSON file, written atomically.
package store

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
)

type Tab struct {
	ID   string `json:"id"`
	Path string `json:"path"`
}

type Draft struct {
	Summary string `json:"summary"`
	Body    string `json:"body"`
}

type Layout struct {
	LeftWidth  int `json:"leftWidth"`
	RightWidth int `json:"rightWidth"`
}

type State struct {
	Tabs      []Tab            `json:"tabs"`
	ActiveTab string           `json:"activeTab"`
	Recents   []string         `json:"recents"`
	Favorites []string         `json:"favorites"`
	Drafts    map[string]Draft `json:"drafts"`
	Layout    Layout           `json:"layout"`
}

const MaxRecents = 20

// EnvStatePath, when set to a non-empty value, overrides the default state
// file location (used by E2E tests).
const EnvStatePath = "GITRAIS_STATE_PATH"

// ErrCorrupt is wrapped by the error Load returns when the state file cannot
// be parsed. The returned State is then the default one.
var ErrCorrupt = errors.New("store: corrupt state file")

type Store struct {
	path string
	mu   sync.Mutex
}

// DefaultPath returns $GITRAIS_STATE_PATH if set, otherwise
// ~/Library/Application Support/Gitrais/state.json.
func DefaultPath() (string, error) {
	if p := os.Getenv(EnvStatePath); p != "" {
		return p, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, "Library", "Application Support", "Gitrais", "state.json"), nil
}

func Open(path string) *Store { return &Store{path: path} }

func defaults() State {
	return State{Tabs: []Tab{}, Recents: []string{}, Favorites: []string{}, Drafts: map[string]Draft{},
		Layout: Layout{LeftWidth: 260, RightWidth: 380}}
}

// normalize replaces nil collections so the JSON never contains null.
func normalize(st *State) {
	if st.Tabs == nil {
		st.Tabs = []Tab{}
	}
	if st.Recents == nil {
		st.Recents = []string{}
	}
	if st.Favorites == nil {
		st.Favorites = []string{}
	}
	if st.Drafts == nil {
		st.Drafts = map[string]Draft{}
	}
}

// Load reads the state. A missing file yields defaults and no error. A corrupt
// file is moved to "<path>.corrupt" (replacing any older copy) so a later
// Update cannot overwrite it; Load then returns defaults and an error wrapping
// ErrCorrupt, which the caller should log.
func (s *Store) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, corrupt, err := s.load()
	if err != nil {
		return st, err
	}
	return st, corrupt
}

// load returns (state, corruptionErr, fatalErr). On corruption the file has
// already been quarantined and the state is the default one; fatalErr is set
// for I/O failures (including a failed quarantine), where nothing may be
// written.
func (s *Store) load() (State, error, error) {
	st := defaults()
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return st, nil, nil
	}
	if err != nil {
		return st, nil, err
	}
	if jerr := json.Unmarshal(b, &st); jerr != nil {
		if rerr := os.Rename(s.path, s.path+".corrupt"); rerr != nil {
			return defaults(), nil, fmt.Errorf("%w: %v (quarantine failed: %v)", ErrCorrupt, jerr, rerr)
		}
		return defaults(), fmt.Errorf("%w: %v (kept as %s.corrupt)", ErrCorrupt, jerr, s.path), nil
	}
	normalize(&st)
	return st, nil, nil
}

// Update loads the state, applies fn and atomically persists the result. It is
// safe for concurrent use. If the existing file is corrupt it is quarantined
// (see Load) and fn runs on the default state; Update does not report that as
// an error, since Load at startup is where the caller learns of it.
func (s *Store) Update(fn func(*State)) (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	st, _, err := s.load()
	if err != nil {
		return st, err
	}
	fn(&st)
	normalize(&st)
	b, err := json.MarshalIndent(st, "", "  ")
	if err != nil {
		return st, err
	}
	return st, writeAtomic(s.path, b)
}

// writeAtomic writes data to a temp file in the same directory, fsyncs it and
// renames it over path, so readers never observe a truncated file.
func writeAtomic(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*") // created 0600
	if err != nil {
		return err
	}
	tmp := f.Name()
	ok := false
	defer func() {
		if !ok {
			f.Close()
			os.Remove(tmp)
		}
	}()
	if _, err := f.Write(data); err != nil {
		return err
	}
	if err := f.Sync(); err != nil {
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	if err := os.Rename(tmp, path); err != nil {
		return err
	}
	ok = true
	if d, err := os.Open(dir); err == nil { // best-effort directory sync
		d.Sync()
		d.Close()
	}
	return nil
}

// AddRecent moves path to the front of Recents, dropping duplicates and
// capping the list at MaxRecents.
func AddRecent(st *State, path string) {
	st.Recents = slices.DeleteFunc(st.Recents, func(p string) bool { return p == path })
	st.Recents = append([]string{path}, st.Recents...)
	if len(st.Recents) > MaxRecents {
		st.Recents = st.Recents[:MaxRecents]
	}
}

// ToggleFavorite adds path to Favorites, or removes it if already present.
func ToggleFavorite(st *State, path string) {
	if i := slices.Index(st.Favorites, path); i >= 0 {
		st.Favorites = slices.Delete(st.Favorites, i, i+1)
		return
	}
	st.Favorites = append(st.Favorites, path)
}
