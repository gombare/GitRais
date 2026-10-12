GIT_VERSION := 2.54.0
export GITRAIS_GIT ?= $(CURDIR)/build/git-dist/bin/git

.PHONY: git helper test test-short dev build package bench

git:
	./scripts/build-git.sh $(GIT_VERSION) build/git-dist

helper:
	go build -o build/bin/askpass-helper ./cmd/askpass-helper

test: helper
	go test ./...
	cd frontend && npm test -- --run

test-short:
	go test -short ./...

dev: helper
	wails dev

build: helper
	wails build -platform darwin/arm64 -clean

package: build
	./scripts/package.sh

bench:
	go test -run '^$$' -bench . -benchtime 1x ./internal/graph/
