#!/usr/bin/env bash
# Generate Formula/hushify.rb for nvteh/homebrew-brew-tools from release checksums.
set -euo pipefail

VERSION="${1:?usage: update-homebrew-formula.sh <version> <checksums-file> <output-file>}"
CHECKSUMS_FILE="${2:?}"
OUTPUT_FILE="${3:?}"

sha_for() {
  local name="$1"
  local line
  line="$(grep -E "[[:space:]]${name}$" "$CHECKSUMS_FILE" || true)"
  if [[ -z "$line" ]]; then
    echo "missing checksum for $name" >&2
    exit 1
  fi
  awk '{print $1}' <<<"$line"
}

BASE="https://github.com/hushify-io/hushify-cli/releases/download/v${VERSION}"
SHA_DARWIN_ARM64="$(sha_for "hushify_${VERSION}_darwin_arm64")"
SHA_DARWIN_AMD64="$(sha_for "hushify_${VERSION}_darwin_amd64")"
SHA_LINUX_ARM64="$(sha_for "hushify_${VERSION}_linux_arm64")"
SHA_LINUX_AMD64="$(sha_for "hushify_${VERSION}_linux_amd64")"

mkdir -p "$(dirname "$OUTPUT_FILE")"
cat >"$OUTPUT_FILE" <<EOF
class Hushify < Formula
  desc "One-time encrypted secret sharing CLI for hushify.io"
  homepage "https://www.hushify.io"
  version "${VERSION}"

  on_macos do
    on_arm do
      url "${BASE}/hushify_${VERSION}_darwin_arm64"
      sha256 "${SHA_DARWIN_ARM64}"
    end
    on_intel do
      url "${BASE}/hushify_${VERSION}_darwin_amd64"
      sha256 "${SHA_DARWIN_AMD64}"
    end
  end

  on_linux do
    on_arm do
      url "${BASE}/hushify_${VERSION}_linux_arm64"
      sha256 "${SHA_LINUX_ARM64}"
    end
    on_intel do
      url "${BASE}/hushify_${VERSION}_linux_amd64"
      sha256 "${SHA_LINUX_AMD64}"
    end
  end

  def install
    os = OS.mac? ? "darwin" : "linux"
    arch = Hardware::CPU.arm? ? "arm64" : "amd64"
    bin.install "hushify_#{version}_#{os}_#{arch}" => "hushify"
  end

  test do
    assert_match version.to_s, shell_output("#{bin}/hushify version")
  end
end
EOF

echo "Wrote $OUTPUT_FILE for hushify ${VERSION}"
