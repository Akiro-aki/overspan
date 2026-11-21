# Overspan build. Every binary is static (CGO off): one file runs on any
# Linux distribution with that CPU architecture, glibc or musl.
GOFLAGS := -trimpath -ldflags "-s -w"
ARCHES  := amd64 arm64 386 arm riscv64

.PHONY: build all test e2e shots clean

build:
	cd server && CGO_ENABLED=0 go build $(GOFLAGS) -o ../dist/overspan ./cmd/overspan

all:
	@for a in $(ARCHES); do \
	  echo "building linux/$$a"; \
	  (cd server && CGO_ENABLED=0 GOOS=linux GOARCH=$$a GOARM=7 go build $(GOFLAGS) -o ../dist/overspan-linux-$$a ./cmd/overspan) || exit 1; \
	done

test:
	cd server && go vet ./... && go test ./...

# needs a live Hyprland or Sway session
e2e: build
	./tests/run-e2e.sh

shots: build
	@cfg=$$(mktemp -d); XDG_CONFIG_HOME=$$cfg ./dist/overspan serve >$$cfg/log 2>&1 & pid=$$!; sleep 1; \
	node tests/shots.mjs replica/clone-screens; rc=$$?; kill $$pid; exit $$rc

clean:
	rm -f dist/overspan dist/overspan-linux-*
