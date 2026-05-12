#!/usr/bin/env sh
set -eu

REPO="${REPO:-RokhmadHD/sc-scraper}"
BINARY_NAME="${BINARY_NAME:-contract-scraper}"
INSTALL_DIR="${INSTALL_DIR:-/usr/local/bin}"
VERSION="${VERSION:-latest}"

need() {
  if ! command -v "$1" >/dev/null 2>&1; then
    echo "error: '$1' is required" >&2
    exit 1
  fi
}

detect_os() {
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux) echo "linux" ;;
    darwin) echo "darwin" ;;
    *) echo "error: unsupported OS '$os'" >&2; exit 1 ;;
  esac
}

detect_arch() {
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) echo "error: unsupported architecture '$arch'" >&2; exit 1 ;;
  esac
}

latest_version() {
  url="https://github.com/$REPO/releases/latest"
  curl -fsSLI "$url" | awk 'BEGIN{IGNORECASE=1} /^location:/ {print $2}' | tr -d '\r' | awk -F/ '{print $NF}'
}

need curl
need tar
need awk
need uname

os="$(detect_os)"
arch="$(detect_arch)"

if [ "$VERSION" = "latest" ]; then
  VERSION="$(latest_version)"
fi

if [ -z "$VERSION" ]; then
  echo "error: could not resolve latest release version" >&2
  exit 1
fi

asset="${BINARY_NAME}_${os}_${arch}.tar.gz"
url="https://github.com/$REPO/releases/download/$VERSION/$asset"
tmp_dir="$(mktemp -d)"

cleanup() {
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

echo "Downloading $BINARY_NAME $VERSION for $os/$arch"
curl -fL "$url" -o "$tmp_dir/$asset"
tar -xzf "$tmp_dir/$asset" -C "$tmp_dir"
chmod +x "$tmp_dir/$BINARY_NAME"

if [ ! -d "$INSTALL_DIR" ]; then
  mkdir -p "$INSTALL_DIR"
fi

target="$INSTALL_DIR/$BINARY_NAME"
if [ -w "$INSTALL_DIR" ]; then
  mv "$tmp_dir/$BINARY_NAME" "$target"
else
  need sudo
  sudo mv "$tmp_dir/$BINARY_NAME" "$target"
fi

echo "Installed $BINARY_NAME to $target"
"$target" --help >/dev/null 2>&1 || true
