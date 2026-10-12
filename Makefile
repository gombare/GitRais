GIT_VERSION := 2.54.0
export GITRAIS_GIT ?= $(CURDIR)/build/git-dist/bin/git

.PHONY: git helper dist-stub test test-short dev build package bench

git:
	./scripts/build-git.sh $(GIT_VERSION) build/git-dist

# main.go embeds frontend/dist: a placeholder lets `go test ./...` compile before a frontend build.
dist-stub:
	@mkdir -p frontend/dist && [ -n "$$(ls -A frontend/dist)" ] || touch frontend/dist/.placeholder

helper:
	go build -o build/bin/askpass-helper ./cmd/askpass-helper

test: helper dist-stub
	go test ./...
	cd frontend && npm test -- --run

test-short: dist-stub
	go test -short ./...

dev: helper
	wails dev

build: helper
	wails build -platform darwin/arm64 -clean

package: build
	./scripts/package.sh

bench:
	go test -run '^$$' -bench . -benchtime 1x ./internal/graph/
