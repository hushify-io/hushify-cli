#!/usr/bin/env bash
# Install hushify CLI on Linux from GitHub releases.
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/hushify-io/hushify-cli/main/install.sh | bash
#
# Optional env:
#   VERSION   release version without leading v (default: latest)
#   PREFIX    install prefix (default: /usr/local or ~/.local)
#   BIN_DIR   install directory for the binary (overrides PREFIX/bin)
set -euo pipefail

REPO="hushify-io/hushify-cli"
BASE_URL="https://github.com/${REPO}/releases"

info() { printf '==> %s\n' "$*"; }
warn() { printf 'warning: %s\n' "$*" >&2; }
die() { printf 'error: %s\n' "$*" >&2; exit 1; }

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || die "missing required command: $1"
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64 | amd64) echo amd64 ;;
    aarch64 | arm64) echo arm64 ;;
    *) die "unsupported architecture: $arch (need amd64 or arm64)" ;;
  esac
}

latest_version() {
  local json tag
  json="$(curl -fsSL "https://api.github.com/repos/${REPO}/releases/latest")"
  tag="$(printf '%s\n' "$json" | grep -o '"tag_name":[[:space:]]*"[^"]*"' | head -1 | cut -d'"' -f4)"
  [[ -n "$tag" ]] || die "could not determine latest release version"
  printf '%s\n' "${tag#v}"
}

install_dir() {
  if [[ -n "${BIN_DIR:-}" ]]; then
    printf '%s\n' "$BIN_DIR"
    return
  fi
  if [[ -n "${PREFIX:-}" ]]; then
    printf '%s\n' "${PREFIX%/}/bin"
    return
  fi
  if [[ "$(id -u)" -eq 0 ]] || [[ -w /usr/local/bin ]]; then
    printf '%s\n' /usr/local/bin
    return
  fi
  printf '%s\n' "${HOME}/.local/bin"
}

main() {
  need_cmd curl
  need_cmd uname
  need_cmd mktemp
  need_cmd install

  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  [[ "$os" == linux ]] || die "this installer supports Linux only (detected: $os)"

  local arch version dest asset tmpdir binary checksums expected
  arch="$(detect_arch)"
  version="${VERSION:-$(latest_version)}"
  version="${version#v}"
  dest="$(install_dir)"
  asset="hushify_${version}_linux_${arch}"

  info "Installing hushify ${version} (${arch}) to ${dest}"

  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  curl -fsSL "${BASE_URL}/download/v${version}/${asset}" -o "${tmpdir}/${asset}"
  curl -fsSL "${BASE_URL}/download/v${version}/checksums.txt" -o "${tmpdir}/checksums.txt"

  expected="$(grep -E "[[:space:]]${asset}$" "${tmpdir}/checksums.txt" | awk '{print $1}')"
  [[ -n "$expected" ]] || die "checksum not found for ${asset}"

  if command -v sha256sum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected" "${tmpdir}/${asset}" | sha256sum -c - >/dev/null
  elif command -v shasum >/dev/null 2>&1; then
    printf '%s  %s\n' "$expected" "${tmpdir}/${asset}" | shasum -a 256 -c - >/dev/null
  else
    die "missing sha256sum or shasum for checksum verification"
  fi

  mkdir -p "$dest"
  binary="${dest}/hushify"
  if [[ -w "$dest" ]]; then
    install -m 755 "${tmpdir}/${asset}" "$binary"
  else
    need_cmd sudo
    sudo install -m 755 "${tmpdir}/${asset}" "$binary"
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
