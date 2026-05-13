#!/usr/bin/env sh
set -eu

REPO="${REPO:-RokhmadHD/sc-scraper}"
BINARY_NAME="${BINARY_NAME:-contract-scraper}"
VERSION="${VERSION:-latest}"
CHAINS_OUTPUT="${CHAINS_OUTPUT:-chains}"
CHAINS_INPUT="${CHAINS_INPUT:-}"
SYNC_CHAINS="${SYNC_CHAINS:-1}"

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
need mktemp

detect_install_dir() {
  if [ -n "${PREFIX:-}" ] && [ -d "$PREFIX/bin" ] && { [ -n "${TERMUX_VERSION:-}" ] || [ "${PREFIX#/data/data/com.termux/}" != "$PREFIX" ]; }; then
    echo "$PREFIX/bin"
    return
  fi
  echo "/usr/local/bin"
}

if [ -z "${INSTALL_DIR:-}" ]; then
  INSTALL_DIR="$(detect_install_dir)"
fi

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

if [ "$SYNC_CHAINS" != "0" ]; then
  if [ -z "$CHAINS_INPUT" ]; then
    CHAINS_INPUT="$tmp_dir/chains.json"
    echo "Downloading chain list"
    curl -fL "https://chainid.network/chains.json" -o "$CHAINS_INPUT"
  fi
  echo "Syncing chains to $CHAINS_OUTPUT"
  if ! "$target" --sync-chains --chains-input "$CHAINS_INPUT" --chains-output "$CHAINS_OUTPUT"; then
    echo "warning: chain sync failed" >&2
  fi
fi
