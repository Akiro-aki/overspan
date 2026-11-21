#!/bin/sh
# Builds Overspan AppImages for every architecture the AppImage project ships
# a runtime for: x86_64, aarch64, i686 and armhf. (RISC-V has none; use the
# tarball there.) The AppImage only wraps the same static binary as the
# tarballs, so it runs on any distribution with FUSE, or anywhere with
# --appimage-extract-and-run.
#
# The official appimagetool and the type-2 runtimes are downloaded once into
# ~/.cache/overspan/appimage-tools. Each runtime must carry a good signature
# from the AppImage project's key (needs gpg); the build stops otherwise.
set -eu
here=$(cd "$(dirname "$0")" && pwd)
root=$(cd "$here/../.." && pwd)
tools=${XDG_CACHE_HOME:-$HOME/.cache}/overspan/appimage-tools
base=https://github.com/AppImage
version=$(sed -n 's/^const Version = "\(.*\)"/\1/p' "$root/server/internal/server/server.go")
mkdir -p "$tools"
command -v gpg >/dev/null 2>&1 || { echo "gpg is needed to check the AppImage runtimes" >&2; exit 1; }

fetch() { [ -s "$tools/$1" ] || curl -fsSL -o "$tools/$1" "$2"; }
fetch appimagetool-x86_64.AppImage "$base/appimagetool/releases/download/continuous/appimagetool-x86_64.AppImage"
chmod +x "$tools/appimagetool-x86_64.AppImage"
fetch signing-pubkey.asc "$base/type2-runtime/releases/download/continuous/signing-pubkey.asc"
keyring=$(mktemp -d)
trap 'rm -rf "$keyring"' EXIT
GNUPGHOME=$keyring gpg -q --import "$tools/signing-pubkey.asc"

for pair in amd64:x86_64 arm64:aarch64 386:i686 arm:armhf; do
  goarch=${pair%%:*} arch=${pair#*:}
  fetch "runtime-$arch" "$base/type2-runtime/releases/download/continuous/runtime-$arch"
  fetch "runtime-$arch.sig" "$base/type2-runtime/releases/download/continuous/runtime-$arch.sig"
  if ! check=$(GNUPGHOME=$keyring gpg --verify "$tools/runtime-$arch.sig" "$tools/runtime-$arch" 2>&1); then
    printf '%s\nruntime-%s: signature check failed\n' "$check" "$arch" >&2
    exit 1
  fi
  echo "runtime-$arch: good signature from the AppImage runtime key"
  bin="$root/dist/overspan-linux-$goarch"
  [ -x "$bin" ] || { echo "missing $bin (run make all)" >&2; exit 1; }

  app=$(mktemp -d)
  install -Dm755 "$bin" "$app/usr/bin/overspan"
  install -Dm755 "$here/AppRun" "$app/AppRun"
  install -Dm644 "$here/overspan.desktop" "$app/overspan.desktop"
  install -Dm644 "$here/overspan.desktop" "$app/usr/share/applications/overspan.desktop"
  # .DirIcon must be a PNG (thumbnailers); the SVG goes in the icon theme too
  install -Dm644 "$here/overspan.png" "$app/overspan.png"
  install -Dm644 "$here/overspan.png" "$app/usr/share/icons/hicolor/256x256/apps/overspan.png"
  install -Dm644 "$root/server/internal/web/static/assets/icon.svg" "$app/usr/share/icons/hicolor/scalable/apps/overspan.svg"
  ln -s overspan.png "$app/.DirIcon"
  chmod -R a+rX "$app" # install -D makes parent dirs with the packager's umask
  out="$root/dist/Overspan-$version-$arch.AppImage"
  ARCH=$arch "$tools/appimagetool-x86_64.AppImage" --appimage-extract-and-run --no-appstream \
    --runtime-file "$tools/runtime-$arch" "$app" "$out" >"$app.log" 2>&1 \
    || { cat "$app.log" >&2; exit 1; }
  rm -rf "$app" "$app.log"
  echo "built $out"
done
