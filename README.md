# Gitrais

Gitrais is a desktop Git client for macOS (Apple silicon), built with
[Wails v2](https://wails.io): a Go backend drives a bundled `git` binary and a
React + TypeScript frontend renders the commit graph, the working-tree panel,
diffs and the repository operations (stage, commit, branches, tags, stash,
fetch, pull, push).

The UI is in Italian; code, comments and commit messages are in English.

## Requirements

- macOS arm64, Xcode command line tools
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
make bench                   # 1M-commit graph benchmark (see make bigrepo)
```

Go tests need the bundled git (`make git`); the Makefile points `GITRAIS_GIT`
at it.

## Layout

- `main.go`: Wails entry point
- `internal/app`: methods bound to the frontend, error mapping, shutdown
- `internal/session`: one open repository (graph loading, serialized writes,
  remote operations, file watcher events)
- `internal/gitexec`, `internal/gitread`, `internal/ops`, `internal/graph`,
  `internal/status`, `internal/diff`, `internal/watch`, `internal/store`,
  `internal/askpass`: git execution, reads, operations, graph index and
  layout, status and diff parsing, FSEvents watcher, UI state, credential
  prompts
- `cmd/askpass-helper`: the `GIT_ASKPASS` helper bundled with the app
- `frontend/`: React + TypeScript UI (Vite, Vitest, Playwright)
- `scripts/`: git build, packaging and E2E scripts

## Clean-room note

Gitrais is an original implementation. It reproduces common Git client
behaviour (git command sequences, their effect on repositories, credential and
hook flows) but contains no code, assets, icons, texts, names or branding
from any other Git client, and talks to no third-party service.
