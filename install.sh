#!/bin/sh
# installs flop, the Flowerpanel CLI, for this machine (macOS or Linux, arm64
# or amd64):
#
#   curl -fsSL https://flowercomputer.com/install.sh | sh
#
# FLOP_VERSION=0.1.0 installs that version (default: the latest);
# FLOP_INSTALL_DIR where it goes (default: ~/.local/bin). Releases are at
# https://dl.flowercomputer.com/flop/v<version>/ (each release publishes them,
# and this script). Running it again updates flop.
set -eu

base=https://dl.flowercomputer.com/flop

fail() {
  echo "flop install: $*" >&2
  exit 1
}

# all in a function, called on the last line: a download cut short runs nothing
main() {
  command -v curl >/dev/null || fail "needs curl"
  command -v tar >/dev/null || fail "needs tar"

  case "$(uname -s)" in
    Darwin) os=darwin ;;
    Linux) os=linux ;;
    *) fail "flop runs on macOS and Linux, not $(uname -s)" ;;
  esac
  case "$(uname -m)" in
    arm64 | aarch64) arch=arm64 ;;
    x86_64 | amd64) arch=amd64 ;;
    *) fail "flop runs on arm64 and amd64, not $(uname -m)" ;;
  esac
  # a shell under Rosetta on Apple silicon reports x86_64: install the native build
  if [ "$os" = darwin ] && [ "$arch" = amd64 ] && [ "$(sysctl -n sysctl.proc_translated 2>/dev/null || true)" = 1 ]; then
    arch=arm64
  fi

  version="${FLOP_VERSION:-}"
  if [ -z "$version" ]; then
    version="$(curl -fsSL "$base/latest")" || fail "couldn't find the latest version at $base/latest"
  fi
  version="${version#v}"
  dir="${FLOP_INSTALL_DIR:-$HOME/.local/bin}"

  name="flop_${version}_${os}_${arch}.tar.gz"
  tmp="$(mktemp -d)"
  trap 'rm -rf "$tmp"' EXIT
  echo "downloading flop $version ($os/$arch)"
  curl -fsSL -o "$tmp/$name" "$base/v$version/$name" || fail "couldn't download $base/v$version/$name (is $version a release?)"
  curl -fsSL -o "$tmp/checksums.txt" "$base/v$version/checksums.txt" || fail "couldn't download $version's checksums"

  want="$(awk -v n="$name" '$2 == n { print $1 }' "$tmp/checksums.txt")"
  if command -v sha256sum >/dev/null; then
    got="$(sha256sum "$tmp/$name" | awk '{ print $1 }')"
  else
    got="$(shasum -a 256 "$tmp/$name" | awk '{ print $1 }')"
  fi
  [ -n "$want" ] && [ "$want" = "$got" ] || fail "$name doesn't match its checksum: not installed"

  tar -xzf "$tmp/$name" -C "$tmp" flop
  mkdir -p "$dir"
  # copied beside it, then renamed over it: a flop running meanwhile keeps its file
  cp "$tmp/flop" "$dir/.flop.$$"
  mv -f "$dir/.flop.$$" "$dir/flop"
  "$dir/flop" version >/dev/null 2>&1 || fail "$dir/flop doesn't run on this machine"
  echo "installed flop $version at $dir/flop"

  case ":$PATH:" in
    *":$dir:"*) ;;
    *)
      echo
      echo "$dir isn't on your PATH. Add it, e.g. in ~/.zshrc or ~/.bashrc:"
      echo "  export PATH=\"$dir:\$PATH\""
      ;;
  esac
}

main "$@"
