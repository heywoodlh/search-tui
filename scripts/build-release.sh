#!/usr/bin/env bash
# Cross-compile release binaries into OUT_DIR, write SHA256SUMS and verify
# how each one is linked.
#
# Usage: scripts/build-release.sh OUT_DIR [VERSION]
#
# All builds use CGO_ENABLED=0 (pure Go), so they are self-contained:
#   - Linux, FreeBSD and NetBSD binaries are fully static and run on any
#     distribution/release of that OS for the architecture.
#   - OpenBSD only permits system calls through its libc, so Go always links
#     libc.so/libpthread.so dynamically there; both ship in every OpenBSD
#     base install. Verification allows exactly those.
#   - macOS links libSystem and Windows binaries are PE files; neither has a
#     static option, and both run on any supported OS version.
set -euo pipefail

out="${1:?usage: build-release.sh OUT_DIR [VERSION]}"
version="${2:-$(tr -d '[:space:]' <"$(dirname "$0")/../VERSION")}"

# os/arch[/arm-version]  -> search-OS-ARCH[.exe]
targets=(
  linux/amd64 linux/arm64 linux/arm/6 linux/riscv64
  freebsd/amd64 freebsd/arm64
  openbsd/amd64 openbsd/arm64
  netbsd/amd64 netbsd/arm64
  darwin/arm64
  windows/amd64 windows/arm64
)

mkdir -p "$out"
for t in "${targets[@]}"; do
  IFS=/ read -r os arch armv <<<"$t"
  name="search-$os-$arch${armv:+v$armv}"
  [[ $os == windows ]] && name+=.exe
  echo "building $name"
  GOOS=$os GOARCH=$arch GOARM=${armv:-} CGO_ENABLED=0 go build -trimpath \
    -ldflags "-s -w -X github.com/heywoodlh/tui-search/internal/cli.Version=$version" \
    -o "$out/$name" ./cmd/search
done

echo "verifying linkage"
fail=0
for f in "$out"/search-*; do
  name=$(basename "$f")
  desc=$(file -b "$f")
  case "$name" in
  search-linux-* | search-freebsd-* | search-netbsd-*)
    if [[ $desc != *"statically linked"* ]]; then
      echo "FAIL $name is not statically linked: $desc" >&2
      fail=1
    else
      echo "ok   $name: statically linked"
    fi
    ;;
  search-openbsd-*)
    needed=$(readelf -d "$f" | sed -n 's/.*Shared library: \[\(.*\)\]/\1/p' | sort | tr '\n' ' ')
    if [[ $needed != "libc.so libpthread.so " && $needed != "libc.so " ]]; then
      echo "FAIL $name links unexpected libraries: $needed" >&2
      fail=1
    else
      echo "ok   $name: base-system libc only ($needed)"
    fi
    ;;
  *) echo "ok   $name: ${desc%%,*}" ;;
  esac
done
[[ $fail == 0 ]] || exit 1

(cd "$out" && sha256sum -- search-* >SHA256SUMS)
echo "built ${#targets[@]} binaries in $out (version $version)"
