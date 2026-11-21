# Overspan build. Every binary is static (CGO off): one file runs on any
# Linux distribution with that CPU architecture, glibc or musl.
GOFLAGS := -trimpath -ldflags "-s -w"
ARCHES  := amd64 arm64 386 arm riscv64

.PHONY: build all appimage windows windows-res test e2e windows-e2e shots clean

build:
	cd server && CGO_ENABLED=0 go build $(GOFLAGS) -o ../dist/overspan ./cmd/overspan

all:
	@for a in $(ARCHES); do \
	  echo "building linux/$$a"; \
	  (cd server && CGO_ENABLED=0 GOOS=linux GOARCH=$$a GOARM=7 go build $(GOFLAGS) -o ../dist/overspan-linux-$$a ./cmd/overspan) || exit 1; \
	done

# AppImages (x86_64, aarch64, i686, armhf) around the same static binaries
appimage: all
	./packaging/appimage/build.sh

# Windows viewer: one static exe for 64-bit Windows 10/11 (x64; Windows on
# Arm runs it through emulation). Cross-compiles from any OS.
WINFLAGS := -trimpath -ldflags "-s -w -H windowsgui"
windows:
	cd windows && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build $(WINFLAGS) -o ../dist/overspan-viewer.exe ./cmd/overspan-viewer
	cd windows && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -o ../dist/winprobe.exe ./tools/winprobe

# icon, manifest and version info -> the .syso the Go linker picks up
windows-res:
	cd windows/res && llvm-windres -O coff -i overspan-viewer.rc -o ../cmd/overspan-viewer/rsrc_windows_amd64.syso

test:
	cd server && go vet ./... && go test ./...
	cd windows && go vet ./internal/client && go test ./internal/client && GOOS=windows go vet ./... 2>&1 | grep -v "possible misuse of unsafe.Pointer\|^#" || true

# needs a live Hyprland or Sway session
e2e: build
	./tests/run-e2e.sh

# needs wine and a live Hyprland or Sway session; nothing appears on screen
windows-e2e:
	./tests/windows-e2e.sh

shots: build
	@cfg=$$(mktemp -d); XDG_CONFIG_HOME=$$cfg ./dist/overspan serve >$$cfg/log 2>&1 & pid=$$!; sleep 1; \
	node tests/shots.mjs replica/clone-screens; rc=$$?; kill $$pid; exit $$rc

clean:
	rm -f dist/overspan dist/overspan-linux-* dist/overspan-viewer.exe dist/winprobe.exe dist/Overspan-*.AppImage
