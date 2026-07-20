#!/bin/sh
# Install hushify CLI on Linux from GitHub releases.
# Usage:
#   curl -fsSL https://www.hushify.io/install | sh
#
# Optional env:
#   VERSION   release version without leading v (default: latest)
#   PREFIX    install prefix (default: /usr/local or ~/.local)
#   BIN_DIR   install directory for the binary (overrides PREFIX/bin)
set -eu

REPO="hushify-io/hushify-cli"
BASE_URL="https://github.com/${REPO}/releases"

info() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo amd64 ;;
    aarch64|arm64) echo arm64 ;;
    *) die "unsupported architecture: $arch (need amd64 or arm64)" ;;
  esac
}

latest_version() {
  json="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest")"
  tag="$(printf '%s\n' "$json" | grep -o '"tag_name":[[:space:]]*"[^"]*"' | head -n 1 | cut -d'"' -f4)"
  if [ -z "$tag" ]; then
    die "could not determine latest release version"
  fi
  printf '%s\n' "${tag#v}"
}

install_dir() {
  if [ -n "${BIN_DIR:-}" ]; then
    printf '%s\n' "$BIN_DIR"
    return
  fi
  if [ -n "${PREFIX:-}" ]; then
    printf '%s\n' "${PREFIX%/}/bin"
    return
  fi
  if [ "$(id -u)" -eq 0 ] || [ -w /usr/local/bin ]; then
    printf '%s\n' /usr/local/bin
    return
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

# Must be global: EXIT traps run after main() returns, so locals are gone.
INSTALL_TMPDIR=""

cleanup() {
  if [ -n "${INSTALL_TMPDIR}" ]; then
    rm -rf "${INSTALL_TMPDIR}"
  fi
}

main() {
  need_cmd curl
  need_cmd uname
  need_cmd mktemp
  need_cmd install

  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  if [ "$os" != linux ]; then
    die "this installer supports Linux only (detected: $os)"
  fi

  arch="$(detect_arch)"
  version="${VERSION:-$(latest_version)}"
  version="${version#v}"
  dest="$(install_dir)"
  asset="hushify_${version}_linux_${arch}"

  info "Installing hushify ${version} (${arch}) to ${dest}"

  INSTALL_TMPDIR="$(mktemp -d)"
  trap cleanup EXIT

  curl -fsSL "${BASE_URL}/download/v${version}/${asset}" -o "${INSTALL_TMPDIR}/${asset}"
  curl -fsSL "${BASE_URL}/download/v${version}/checksums.txt" -o "${INSTALL_TMPDIR}/checksums.txt"

  expected="$(grep -E "[[:space:]]${asset}$" "${INSTALL_TMPDIR}/checksums.txt" | awk '{print $1}')"
  if [ -z "$expected" ]; then
    die "checksum not found for ${asset}"
  fi

  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected" "${INSTALL_TMPDIR}/${asset}" | sha256sum -c - >/dev/null
  elif command -v shasum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected" "${INSTALL_TMPDIR}/${asset}" | shasum -a 256 -c - >/dev/null
  else
    die "missing sha256sum or shasum for checksum verification"
  fi

  mkdir -p "$dest"
  binary="${dest}/hushify"
  if [ -w "$dest" ]; then
    install -m 755 "${INSTALL_TMPDIR}/${asset}" "$binary"
  else
    need_cmd sudo
    sudo install -m 755 "${INSTALL_TMPDIR}/${asset}" "$binary"
  fi

  info "Installed ${binary}"
  if ! command -v hushify >/dev/null 2>&1; then
    warn "${dest} is not on your PATH"
    warn "Add it with: export PATH=\"${dest}:\$PATH\""
  else
    info "hushify $(hushify version 2>/dev/null | awk '{print $2}')"
  fi
}

main "$@"
