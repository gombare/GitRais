# GitRais

A fast desktop Git client for macOS (Apple silicon), built with
[Wails v2](https://wails.io): a Go backend drives a bundled `git` binary and a
React + TypeScript frontend renders the commit graph, the working tree, diffs
and the everyday repository operations.

The UI is in Italian; code, comments and commit messages are in English.

> **Status:** early development (0.1.0). The Core (sub-project 1) is complete;
> builds are ad-hoc signed and not notarized.

## Features

- **Commit graph** drawn on a virtualized canvas, streamed from the backend
  and laid out in Go: the first rows show in under a second and scrolling
  stays smooth on repositories with a million commits.
- **Working tree**: stage and unstage whole files, single hunks or single
  lines; discard changes; commit and amend, with hook output shown on failure;
  drafts of the commit message kept per repository.
- **Diffs** in Monaco, side by side or inline, for the working tree and for
  any commit.
- **Branches and tags**: create, rename, delete, set upstream, check out local,
  remote or detached; ahead/behind counts per branch.
- **Stash**: push (optionally with untracked files), apply, pop, drop.
- **Remotes**: fetch, pull (merge, rebase or fast-forward only, with an opt-in
  autostash), push (including force push and new branches), clone and init,
  with live progress and cancel.
- **Credentials**: HTTPS username/password prompts in the app through its own
  `GIT_ASKPASS` helper, stored in the macOS keychain; SSH through your agent.
  Your own git credential helpers keep working.
- **Background fetch** (on by default, configurable, every 60 s by default)
  that never prompts for credentials and yields to anything you do.
- **Live refresh**: an FSEvents watcher reflects changes made outside the app
  (a commit from the terminal, an edited file) within about a second.
- **Tabs**, recent and favourite repositories, panel layout: all restored at
  the next launch.
- **Recovery**: a stale `index.lock` is detected and can be removed safely.
- Every git command the app runs is written to an activity log
  (`~/Library/Logs/Gitrais/activity.log`), with credentials removed from URLs.

## Roadmap

1. **Core**: done.
2. **Advanced Git**: merge and rebase, interactive rebase, 3-way conflict
   editor, cherry-pick, revert, reset, undo/redo, blame, file history.
3. **Workflow**: integrated terminal, command palette and fuzzy finder,
   configurable keybindings, submodules, worktrees, LFS, profiles.
4. **Integrations**: GitHub, GitLab and Bitbucket pull requests and issues.

## Requirements

- macOS on Apple silicon, Xcode command line tools
- Go (see `go.mod`), Node.js and npm
- Wails CLI: `go install github.com/wailsapp/wails/v2/cmd/wails@latest`
  (the Makefile also finds it in `$(go env GOPATH)/bin`)

## Build and run

```sh
make git        # build the bundled git into build/git-dist (once)
make dev        # live development (wails dev) with the askpass helper
make build      # production build: build/bin/Gitrais.app
make package    # build, then embed git and the askpass helper and ad-hoc sign the .app
```

A plain `make build` app has no bundled git: run `make package` to get an app
that works on its own.

## Test

```sh
make dist-stub && go vet ./...
go test -race -short ./...   # or: make test-short
make test                    # Go tests (including file-watcher tests) and Vitest
cd frontend && npx tsc --noEmit && npm test -- --run && npm run build
make e2e                     # Playwright flows against wails dev
make bigrepo && make bench   # 1M-commit graph benchmark
```

Go tests need the bundled git (`make git`); the Makefile points `GITRAIS_GIT`
at it.

## Architecture

- One Go process; one `Session` per open tab. All writes go through the bundled
  git 2.54.0, one write at a time per repository; reads run concurrently.
- The graph is computed in Go while `git log` streams, and served to the
  frontend in pages of rows.
- The frontend never runs git and never builds paths: it receives DTOs from the
  backend and listens to events for progress, refresh and credential prompts.
- Only `internal/app` imports Wails, so the rest of the backend is plain Go and
  testable on its own.

### Layout

- `main.go`: Wails entry point
- `internal/app`: methods bound to the frontend, error mapping, shutdown
- `internal/session`: one open repository (graph loading, serialized writes,
  remote operations, file watcher events, background fetch)
- `internal/gitexec`: git execution, errors, progress, activity log
- `internal/gitread`, `internal/status`, `internal/diff`: refs, commits,
  status and diff parsing
- `internal/ops`: staging, commit, branches, tags, stash, remotes
- `internal/graph`: graph index and lane layout
- `internal/watch`: FSEvents watcher
- `internal/store`: persisted UI state
- `internal/askpass`, `cmd/askpass-helper`: credential prompts
- `cmd/genrepo`: synthetic repository generator for the benchmark
- `frontend/`: React + TypeScript UI (Vite, Zustand, Monaco, Vitest, Playwright)
- `scripts/`: git build, packaging and E2E scripts

## Clean-room note

GitRais is an original implementation. It reproduces common Git client
behaviour (git command sequences, their effect on repositories, credential and
hook flows) but contains no code, assets, icons, texts, names or branding
from any other Git client, and talks to no third-party service.

## License

Apache License 2.0: see [LICENSE](LICENSE).
