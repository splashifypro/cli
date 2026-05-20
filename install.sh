#!/bin/sh
# splashify CLI installer for Linux and macOS.
#
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/splashifypro/cli/main/install.sh | sh
#
# Environment overrides:
#   SPLASHIFY_VERSION       e.g. v0.1.0 (default: latest release)
#   SPLASHIFY_INSTALL_DIR   e.g. /opt/bin (default: /usr/local/bin, falling back to ~/.local/bin)
#
# What this script does:
#   1. Detect OS (linux | darwin) and arch (x86_64 | arm64).
#   2. Resolve the latest v* release on GitHub (or honour SPLASHIFY_VERSION).
#   3. Download the matching tarball + SHA256SUMS.
#   4. Verify the checksum.
#   5. Extract `splashify` and install it on PATH.
#
# Exit codes:
#   0 success — splashify is on PATH (or instructions printed).
#   1 generic failure.
#   2 unsupported OS/arch.
set -eu

REPO="splashifypro/cli"

say()  { printf '%s\n' "$*"; }
warn() { printf '%s\n' "$*" >&2; }
die()  { warn "error: $*"; exit "${2:-1}"; }

need() {
  command -v "$1" >/dev/null 2>&1 || die "missing required tool: $1"
}

need curl
need tar
need uname

# ── 1. Detect OS + arch ───────────────────────────────────────────────────────
os=$(uname -s | tr '[:upper:]' '[:lower:]')
case "$os" in
  linux)  goos=linux ;;
  darwin) goos=darwin ;;
  *) die "unsupported OS: $os" 2 ;;
esac

uarch=$(uname -m)
case "$uarch" in
  x86_64|amd64) goarch=x86_64 ;;
  arm64|aarch64) goarch=arm64 ;;
  *) die "unsupported architecture: $uarch" 2 ;;
esac

# ── 2. Resolve version ───────────────────────────────────────────────────────
version="${SPLASHIFY_VERSION:-}"
if [ -z "$version" ]; then
  say "Looking up latest splashify release..."
  api="https://api.github.com/repos/${REPO}/releases/latest"
  release=$(curl -fsSL "$api") || die "could not reach GitHub API"
  version=$(printf '%s' "$release" \
    | tr ',' '\n' \
    | grep -E '"tag_name"\s*:\s*"v' \
    | head -n 1 \
    | sed -E 's/.*"tag_name"\s*:\s*"(v[^"]+)".*/\1/')
  [ -n "$version" ] || die "no published splashify CLI release found"
fi
case "$version" in
  v*) ;;
  *) version="v${version}" ;;
esac
tag="${version}"
# GoReleaser names assets with the bare semver ({{ .Version }}, no `v` prefix)
# while the release tag keeps the `v`. Keep both forms.
asset_version="${version#v}"
say "Installing splashify ${version} for ${goos}/${goarch}"

# ── 3. Download artefacts ────────────────────────────────────────────────────
asset="splashify_${asset_version}_${goos}_${goarch}.tar.gz"
base="https://github.com/${REPO}/releases/download/${tag}"

tmp=$(mktemp -d 2>/dev/null || mktemp -d -t splashify)
trap 'rm -rf "$tmp"' EXIT

say "Downloading ${asset}..."
curl -fsSL -o "$tmp/$asset"        "$base/$asset"        || die "download failed: $asset"
curl -fsSL -o "$tmp/SHA256SUMS"    "$base/SHA256SUMS"    || die "download failed: SHA256SUMS"

# ── 4. Verify checksum ───────────────────────────────────────────────────────
( cd "$tmp" && {
    if command -v sha256sum >/dev/null 2>&1; then
      grep " $asset\$" SHA256SUMS | sha256sum -c -
    else
      # macOS: shasum -a 256 -c expects the same BSD/GNU-compatible format.
      grep " $asset\$" SHA256SUMS | shasum -a 256 -c -
    fi
} ) || die "checksum verification failed for $asset"

# ── 5. Extract and install ───────────────────────────────────────────────────
tar -C "$tmp" -xzf "$tmp/$asset"
[ -f "$tmp/splashify" ] || die "expected splashify binary not found in tarball"
chmod +x "$tmp/splashify"

dest_dir="${SPLASHIFY_INSTALL_DIR:-}"
sudo_prefix=""
if [ -z "$dest_dir" ]; then
  if [ -w /usr/local/bin ]; then
    dest_dir=/usr/local/bin
  elif command -v sudo >/dev/null 2>&1 && [ -d /usr/local/bin ]; then
    dest_dir=/usr/local/bin
    sudo_prefix="sudo"
  else
    dest_dir="$HOME/.local/bin"
    mkdir -p "$dest_dir"
  fi
fi

say "Installing to ${dest_dir}/splashify"
$sudo_prefix mv "$tmp/splashify" "$dest_dir/splashify" || die "could not write to $dest_dir"

# ── 6. PATH hint + next step ─────────────────────────────────────────────────
case ":$PATH:" in
  *":$dest_dir:"*) ;;
  *)
    warn ""
    warn "$dest_dir is not on your PATH. Add it with:"
    warn "  export PATH=\"$dest_dir:\$PATH\""
    warn "(persist this by appending the line to ~/.bashrc or ~/.zshrc)"
    ;;
esac

say ""
say "✓ splashify installed."
say "  Next: run \`splashify connect\` and paste an access token from"
say "        Splashify Pro → Settings → Developer → Access Tokens."
