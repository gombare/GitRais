GIT_VERSION := 2.54.0
export GITRAIS_GIT ?= $(CURDIR)/build/git-dist/bin/git

.PHONY: git helper dist-stub test test-short e2e dev build package bench bigrepo

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

e2e:
	./scripts/e2e.sh

dev: helper
	wails dev

build: helper
	wails build -platform darwin/arm64 -clean

# `wails build -clean` wipes build/bin, so the helper is rebuilt (fresh make
# invocation: phony targets run once per invocation) before packaging.
package: build
	$(MAKE) helper
	./scripts/package.sh

# 1M-commit synthetic repository for `make bench` (about 800 MB, several minutes).
# commit-graph instead of gc: same incremental --topo-order, no full repack.
bigrepo:
	rm -rf build/bigrepo && $(GITRAIS_GIT) init -q build/bigrepo
	go run ./cmd/genrepo -n 1000000 | $(GITRAIS_GIT) -C build/bigrepo fast-import --quiet
	$(GITRAIS_GIT) -C build/bigrepo symbolic-ref HEAD refs/heads/main
	$(GITRAIS_GIT) -C build/bigrepo commit-graph write --reachable

# Uses build/bigrepo when present, otherwise generates 1M commits in a temp dir.
bench: dist-stub
	GITRAIS_PERF=1 $(if $(wildcard build/bigrepo),GITRAIS_BIGREPO=$(CURDIR)/build/bigrepo) \
		go test -run TestPerfBigRepo -bench . -v -timeout 30m ./internal/graph/
