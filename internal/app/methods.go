package app

import (
	"context"
	"path/filepath"
	"slices"
	"strings"

	"gitrais/internal/diff"
	"gitrais/internal/gitexec"
	"gitrais/internal/gitread"
	"gitrais/internal/graph"
	"gitrais/internal/ops"
	"gitrais/internal/session"
	"gitrais/internal/status"
	"gitrais/internal/store"
)

func (a *App) GetState() (store.State, error) {
	st, err := a.store.Load()
	return st, uiErr(err)
}

func tabInfo(id string, s *session.Session) TabInfo {
	return TabInfo{ID: id, Path: s.Dir, Name: filepath.Base(s.Dir)}
}

// findTab returns the open tab whose repository is at path, if any.
func (a *App) findTab(path string) (TabInfo, bool) {
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		real = path
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	for id, e := range a.sessions {
		if e.s.Dir == real || e.s.Dir == path {
			return tabInfo(id, e.s), true
		}
	}
	return TabInfo{}, false
}

// openSession opens a session with the given tab ID. When another tab already
// shows the same repository (e.g. path was a subdirectory), the new session is
// discarded and that tab is returned instead.
func (a *App) openSession(id, path string) (TabInfo, error) {
	e := &tabEntry{}
	emit := func(ev string, d any) {
		e.mu.RLock()
		defer e.mu.RUnlock()
		if !e.dropped {
			a.emit(ev, d)
		}
	}
	s, err := session.Open(id, path, a.runner, emit)
	if err != nil {
		return TabInfo{}, err
	}
	e.s = s
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		e.drop()
		s.Close()
		return TabInfo{}, session.ErrClosed
	}
	for oid, o := range a.sessions {
		if o.s.Dir == s.Dir {
			a.closeLocked(e)
			a.mu.Unlock()
			return tabInfo(oid, o.s), nil
		}
	}
	a.sessions[id] = e
	a.mu.Unlock()
	return tabInfo(id, s), nil
}

// RestoreTabs reopens the tabs saved in the state. Tabs whose repository can
// no longer be opened (or that duplicate another tab) are removed from the
// state. Already open tabs are reused, so calling it twice is harmless.
func (a *App) RestoreTabs() ([]TabInfo, error) {
	if err := a.ready(); err != nil {
		return nil, uiErr(err) // never drop saved tabs because git is missing
	}
	st, err := a.store.Load()
	if err != nil {
		return nil, uiErr(err)
	}
	tabs := []TabInfo{}
	drop := map[string]bool{}
	for _, t := range st.Tabs {
		a.mu.Lock()
		e, open := a.sessions[t.ID]
		a.mu.Unlock()
		if open {
			tabs = append(tabs, tabInfo(t.ID, e.s))
			continue
		}
		if a.beforeRestoreOpen != nil {
			a.beforeRestoreOpen(t.ID)
		}
		ti, err := a.openSession(t.ID, t.Path)
		if a.interrupted(err) {
			// The app is shutting down: the saved tabs are still valid, so
			// leave the store untouched.
			return tabs, uiErr(session.ErrClosed)
		}
		if err != nil || ti.ID != t.ID {
			drop[t.ID] = true
			continue
		}
		tabs = append(tabs, ti)
	}
	if len(drop) > 0 {
		if a.interrupted(nil) {
			return tabs, uiErr(session.ErrClosed)
		}
		_, err = a.store.Update(func(s *store.State) {
			s.Tabs = slices.DeleteFunc(s.Tabs, func(t store.Tab) bool { return drop[t.ID] })
			if drop[s.ActiveTab] {
				s.ActiveTab = ""
				if len(s.Tabs) > 0 {
					s.ActiveTab = s.Tabs[0].ID
				}
			}
		})
	}
	return tabs, uiErr(err)
}

// OpenRepo opens the repository containing path (absolute, InvalidPath
// otherwise) in a new tab (or returns the tab already showing it), makes it
// active and adds it to the recents.
func (a *App) OpenRepo(path string) (TabInfo, error) {
	if err := a.ready(); err != nil {
		return TabInfo{}, uiErr(err)
	}
	if path == "" || !filepath.IsAbs(path) {
		return TabInfo{}, uiErr(errInvalidPath)
	}
	if ti, ok := a.findTab(path); ok {
		return ti, nil
	}
	id := newID()
	ti, err := a.openSession(id, path)
	if err != nil {
		return TabInfo{}, uiErr(err)
	}
	if ti.ID != id {
		return ti, nil // same repository as an existing tab
	}
	_, err = a.store.Update(func(st *store.State) {
		st.Tabs = append(st.Tabs, store.Tab{ID: ti.ID, Path: ti.Path})
		st.ActiveTab = ti.ID
		store.AddRecent(st, ti.Path)
	})
	return ti, uiErr(err)
}

// PickDirectory shows the native directory picker; "" if cancelled.
func (a *App) PickDirectory(title string) (string, error) {
	if a.pickDir == nil {
		return "", nil
	}
	p, err := a.pickDir(title)
	return p, uiErr(err)
}

// CloseTab removes the tab. Its session is closed in the background (it may
// take seconds) and none of its events is emitted after CloseTab returns.
func (a *App) CloseTab(tab string) error {
	a.mu.Lock()
	if a.closed {
		a.mu.Unlock()
		return nil
	}
	if e, ok := a.sessions[tab]; ok {
		delete(a.sessions, tab)
		a.closeLocked(e)
	}
	a.mu.Unlock()
	_, err := a.store.Update(func(st *store.State) {
		st.Tabs = slices.DeleteFunc(st.Tabs, func(t store.Tab) bool { return t.ID == tab })
		if st.ActiveTab == tab {
			st.ActiveTab = ""
			if len(st.Tabs) > 0 {
				st.ActiveTab = st.Tabs[len(st.Tabs)-1].ID
			}
		}
	})
	return uiErr(err)
}

func (a *App) SetActiveTab(tab string) error {
	_, err := a.store.Update(func(st *store.State) { st.ActiveTab = tab })
	return uiErr(err)
}

func (a *App) ToggleFavorite(path string) (store.State, error) {
	st, err := a.store.Update(func(st *store.State) { store.ToggleFavorite(st, path) })
	return st, uiErr(err)
}

// SaveDraft stores the commit message draft of a repository (keyed by its
// path); an empty draft deletes it.
func (a *App) SaveDraft(path string, d store.Draft) error {
	_, err := a.store.Update(func(st *store.State) {
		if d.Summary == "" && d.Body == "" {
			delete(st.Drafts, path)
		} else {
			st.Drafts[path] = d
		}
	})
	return uiErr(err)
}

func (a *App) SaveLayout(l store.Layout) error {
	_, err := a.store.Update(func(st *store.State) { st.Layout = l })
	return uiErr(err)
}

// with runs fn on the tab's session, converting the error for the frontend.
func with[T any](a *App, tab string, fn func(*session.Session) (T, error)) (T, error) {
	s, err := a.sess(tab)
	if err != nil {
		var zero T
		return zero, uiErr(err)
	}
	v, err := fn(s)
	if err != nil {
		var zero T
		return zero, uiErr(err)
	}
	return v, nil
}

func do(a *App, tab string, fn func(*session.Session) error) error {
	_, err := with(a, tab, func(s *session.Session) (struct{}, error) { return struct{}{}, fn(s) })
	return err
}

func (a *App) GraphInfo(tab string) (GraphInfo, error) {
	return with(a, tab, func(s *session.Session) (GraphInfo, error) {
		n, done := s.GraphInfo()
		return GraphInfo{Rows: n, Done: done}, nil
	})
}

// maxRowsPerCall bounds one Rows call (the frontend asks for a viewport).
const maxRowsPerCall = 10000

func (a *App) Rows(tab string, start, count int) ([]RowDTO, error) {
	// Clamp here so start+count can never overflow inside graph.Index.Rows.
	start = max(start, 0)
	count = min(max(count, 0), maxRowsPerCall)
	return with(a, tab, func(s *session.Session) ([]RowDTO, error) {
		rows := s.Rows(start, count)
		out := make([]RowDTO, len(rows))
		for i, r := range rows {
			out[i] = toRowDTO(r)
		}
		return out, nil
	})
}

// FindRow returns the row index of hash, or -1.
func (a *App) FindRow(tab, hash string) (int, error) {
	return with(a, tab, func(s *session.Session) (int, error) { return s.Find(hash), nil })
}

func (a *App) Refs(tab string) (gitread.Refs, error) {
	return with(a, tab, func(s *session.Session) (gitread.Refs, error) { return s.Refs(a.ctx) })
}

func (a *App) Stashes(tab string) ([]graph.Stash, error) {
	return with(a, tab, func(s *session.Session) ([]graph.Stash, error) { return s.Stashes(a.ctx) })
}

func (a *App) Status(tab string) (status.Status, error) {
	return with(a, tab, func(s *session.Session) (status.Status, error) { return s.Status(a.ctx) })
}

// CommitDetail takes a full commit hash (InvalidHash otherwise).
func (a *App) CommitDetail(tab, hash string) (gitread.CommitDetail, error) {
	return with(a, tab, func(s *session.Session) (gitread.CommitDetail, error) { return s.Commit(a.ctx, hash) })
}

func (a *App) WipDiff(tab string, req ops.WipDiffRequest) (diff.FileDiff, error) {
	return with(a, tab, func(s *session.Session) (diff.FileDiff, error) { return s.WipDiff(a.ctx, req) })
}

func (a *App) CommitFileDiff(tab, hash, path, oldPath string) (diff.FileDiff, error) {
	return with(a, tab, func(s *session.Session) (diff.FileDiff, error) {
		return s.CommitFileDiff(a.ctx, hash, path, oldPath)
	})
}

func (a *App) LastCommitMessage(tab string) (string, error) {
	return with(a, tab, func(s *session.Session) (string, error) { return s.LastCommitMessage(a.ctx) })
}

func (a *App) Stage(tab string, paths []string) error {
	return do(a, tab, func(s *session.Session) error { return s.Stage(a.ctx, paths) })
}

func (a *App) Unstage(tab string, paths []string) error {
	return do(a, tab, func(s *session.Session) error { return s.Unstage(a.ctx, paths) })
}

func (a *App) StageAll(tab string) error {
	return do(a, tab, func(s *session.Session) error { return s.StageAll(a.ctx) })
}

func (a *App) UnstageAll(tab string) error {
	return do(a, tab, func(s *session.Session) error { return s.UnstageAll(a.ctx) })
}

func (a *App) Discard(tab string, tracked, untracked []string) error {
	return do(a, tab, func(s *session.Session) error { return s.Discard(a.ctx, tracked, untracked) })
}

func (a *App) StageLines(tab string, req session.StageLinesRequest) error {
	return do(a, tab, func(s *session.Session) error { return s.StageLines(a.ctx, req) })
}

// Commit returns the new commit's hash.
func (a *App) Commit(tab string, o ops.CommitOptions) (string, error) {
	return with(a, tab, func(s *session.Session) (string, error) { return s.CommitChanges(a.ctx, o) })
}

func (a *App) CreateBranch(tab, name, start string, checkout bool) error {
	return do(a, tab, func(s *session.Session) error { return s.CreateBranch(a.ctx, name, start, checkout) })
}

func (a *App) RenameBranch(tab, oldName, newName string) error {
	return do(a, tab, func(s *session.Session) error { return s.RenameBranch(a.ctx, oldName, newName) })
}

func (a *App) DeleteBranch(tab, name string, force bool) error {
	return do(a, tab, func(s *session.Session) error { return s.DeleteBranch(a.ctx, name, force) })
}

func (a *App) DeleteRemoteBranch(tab, remote, branch string) error {
	return do(a, tab, func(s *session.Session) error { return s.DeleteRemoteBranch(a.ctx, remote, branch) })
}

func (a *App) SetUpstream(tab, branch, upstream string) error {
	return do(a, tab, func(s *session.Session) error { return s.SetUpstream(a.ctx, branch, upstream) })
}

func (a *App) Checkout(tab string, t ops.CheckoutTarget) error {
	return do(a, tab, func(s *session.Session) error { return s.Checkout(a.ctx, t) })
}

func (a *App) CreateTag(tab, name, target, message string) error {
	return do(a, tab, func(s *session.Session) error { return s.CreateTag(a.ctx, name, target, message) })
}

func (a *App) DeleteTag(tab, name string) error {
	return do(a, tab, func(s *session.Session) error { return s.DeleteTag(a.ctx, name) })
}

func (a *App) PushTag(tab, remote, name string) error {
	return do(a, tab, func(s *session.Session) error { return s.PushTag(a.ctx, remote, name) })
}

func (a *App) StashPush(tab, message string, untracked bool) error {
	return do(a, tab, func(s *session.Session) error { return s.StashPush(a.ctx, message, untracked) })
}

// StashApply, StashPop and StashDrop take the stash commit HASH
// (graph.Stash.Hash / RowDTO.Hash of a stash row), not "stash@{N}": the
// index shifts when stashes change, the hash does not.
func (a *App) StashApply(tab, ref string) error {
	return do(a, tab, func(s *session.Session) error { return s.StashApply(a.ctx, ref) })
}

func (a *App) StashPop(tab, ref string) error {
	return do(a, tab, func(s *session.Session) error { return s.StashPop(a.ctx, ref) })
}

func (a *App) StashDrop(tab, ref string) error {
	return do(a, tab, func(s *session.Session) error { return s.StashDrop(a.ctx, ref) })
}

func (a *App) Fetch(tab string) error {
	return do(a, tab, func(s *session.Session) error { return s.Fetch() })
}

func (a *App) Pull(tab string, mode ops.PullMode) error {
	return do(a, tab, func(s *session.Session) error { return s.Pull(mode) })
}

func (a *App) Push(tab string, force bool) error {
	return do(a, tab, func(s *session.Session) error { return s.PushCurrent(force) })
}

// CancelOp cancels the tab's running remote operation (it then fails with
// kind Cancelled).
func (a *App) CancelOp(tab string) error {
	return do(a, tab, func(s *session.Session) error { s.CancelOp(); return nil })
}

// repoNameFromURL derives the directory name for a clone from a remote URL:
// the last path segment without a trailing ".git" (also for scp-like
// "git@host:owner/repo.git"). Empty, "." / ".." and names containing path
// separators are rejected.
func repoNameFromURL(url string) (string, error) {
	u := strings.TrimRight(strings.TrimSpace(url), "/\\")
	if i := strings.LastIndexAny(u, "/\\:"); i >= 0 {
		u = u[i+1:]
	}
	u = strings.TrimSuffix(u, ".git")
	if u == "" || u == "." || u == ".." || strings.ContainsAny(u, "/\\") || strings.HasPrefix(u, "-") {
		return "", errInvalidPath
	}
	return u, nil
}

// CloneRepo clones url into parentDir (absolute) under a directory name
// derived from the URL, and opens it. The frontend never builds paths.
// Progress is emitted as op:progress with tab "" and op "clone". Only one
// clone runs at a time (OpInProgress otherwise); CancelClone aborts it.
func (a *App) CloneRepo(url, parentDir string) (TabInfo, error) {
	if err := a.ready(); err != nil {
		return TabInfo{}, uiErr(err)
	}
	if parentDir == "" || !filepath.IsAbs(parentDir) {
		return TabInfo{}, uiErr(errInvalidPath)
	}
	name, err := repoNameFromURL(url)
	if err != nil {
		return TabInfo{}, uiErr(err)
	}
	dest := filepath.Join(parentDir, name)
	a.cloneMu.Lock()
	if a.cloneCancel != nil {
		a.cloneMu.Unlock()
		return TabInfo{}, uiErr(session.ErrOpInProgress)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.cloneCancel = cancel
	a.cloneMu.Unlock()
	defer func() {
		a.cloneMu.Lock()
		a.cloneCancel = nil
		a.cloneMu.Unlock()
		cancel()
	}()
	err = ops.Clone(ctx, a.runner, url, dest, func(p gitexec.Progress) {
		a.emit(session.EventOpProgress, session.OpProgress{Op: "clone", Progress: p})
	})
	if err != nil {
		return TabInfo{}, uiErr(err)
	}
	return a.OpenRepo(dest)
}

// CancelClone aborts the running clone, if any.
func (a *App) CancelClone() error {
	a.cloneMu.Lock()
	defer a.cloneMu.Unlock()
	if a.cloneCancel != nil {
		a.cloneCancel()
	}
	return nil
}

// InitRepo creates a repository in dir (absolute; created if missing) and opens it.
func (a *App) InitRepo(dir string) (TabInfo, error) {
	if err := a.ready(); err != nil {
		return TabInfo{}, uiErr(err)
	}
	if err := ops.Init(a.ctx, a.runner, dir); err != nil {
		return TabInfo{}, uiErr(err)
	}
	return a.OpenRepo(dir)
}
