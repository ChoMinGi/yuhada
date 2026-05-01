#!/usr/bin/env bash
# Tailwind CSS CLI standalone 다운로드.
# Node 의존성 없이 CSS 빌드 가능. 로컬 개발(macOS) + Linux 배포 둘 다 지원.
#
# 출력: tailwind/bin/tailwindcss-<os>-<arch>
# Makefile의 css 타겟이 호출.

set -euo pipefail

DEST_DIR="$(cd "$(dirname "$0")/.." && pwd)/tailwind/bin"
mkdir -p "$DEST_DIR"

# 버전 고정 (v4 stable)
VERSION="${TAILWIND_VERSION:-v4.1.15}"

os="$(uname -s | tr '[:upper:]' '[:lower:]')"
arch="$(uname -m)"

case "$os" in
  darwin)  platform="macos" ;;
  linux)   platform="linux" ;;
  *) echo "Unsupported OS: $os"; exit 1 ;;
esac

case "$arch" in
  arm64|aarch64) pkg_arch="arm64" ;;
  x86_64|amd64)  pkg_arch="x64" ;;
  *) echo "Unsupported arch: $arch"; exit 1 ;;
esac

asset="tailwindcss-${platform}-${pkg_arch}"
url="https://github.com/tailwindlabs/tailwindcss/releases/download/${VERSION}/${asset}"
out="${DEST_DIR}/${asset}"

# 이미 있으면 스킵
if [[ -x "$out" ]]; then
  echo "✓ Tailwind CLI already installed: $out"
  exit 0
fi

echo "Downloading $url"
curl -fsSL -o "$out" "$url"
chmod +x "$out"

# 호환 심볼릭 이름
ln -sf "$asset" "${DEST_DIR}/tailwindcss"
echo "✓ Installed: $out"
"$out" --help >/dev/null 2>&1 && echo "✓ Version check OK"
