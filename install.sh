#!/bin/sh
set -eu

# swe-swe installer
# Usage: curl -fsSL https://raw.githubusercontent.com/choonkeat/swe-swe/main/install.sh | sh

PACKAGE_NAME="swe-swe"
BIN_NAME="swe-swe"

main() {
  detect_platform
  fetch_latest_version
  download_and_install
  print_success
}

detect_platform() {
  OS="$(uname -s)"
  ARCH="$(uname -m)"

  case "$OS" in
    Linux)   NPM_OS="linux" ;;
    Darwin)  NPM_OS="darwin" ;;
    MINGW*|MSYS*|CYGWIN*) NPM_OS="win32" ;;
    *)
      echo "Error: Unsupported OS: $OS" >&2
      exit 1
      ;;
  esac

  case "$ARCH" in
    x86_64|amd64)  NPM_ARCH="x64" ;;
    arm64|aarch64) NPM_ARCH="arm64" ;;
    *)
      echo "Error: Unsupported architecture: $ARCH" >&2
      exit 1
      ;;
  esac

  PLATFORM="${NPM_OS}-${NPM_ARCH}"
  SCOPE_PKG="@choonkeat/${BIN_NAME}-${PLATFORM}"
  echo "Detected platform: ${PLATFORM}"
}

fetch_latest_version() {
  echo "Fetching latest version..."
  VERSION=$(curl -fsSL "https://registry.npmjs.org/${PACKAGE_NAME}/latest" | sed -n 's/.*"version":"\([^"]*\)".*/\1/p')
  if [ -z "$VERSION" ]; then
    echo "Error: Could not determine latest version" >&2
    exit 1
  fi
  echo "Latest version: ${VERSION}"
}

download_and_install() {
  # npm scoped package tarball URL: @scope/name -> @scope/name/-/name-version.tgz
  PKG_BASE="${BIN_NAME}-${PLATFORM}"
  TARBALL_URL="https://registry.npmjs.org/${SCOPE_PKG}/-/${PKG_BASE}-${VERSION}.tgz"

  TMPDIR=$(mktemp -d)
  trap 'rm -rf "$TMPDIR"' EXIT

  echo "Downloading ${SCOPE_PKG}@${VERSION}..."
  curl -fsSL "$TARBALL_URL" -o "$TMPDIR/pkg.tgz"

  echo "Extracting..."
  tar xzf "$TMPDIR/pkg.tgz" -C "$TMPDIR"

  BIN_SRC="$TMPDIR/package/bin/${BIN_NAME}"
  if [ "$NPM_OS" = "win32" ]; then
    BIN_SRC="${BIN_SRC}.exe"
  fi

  if [ ! -f "$BIN_SRC" ]; then
    echo "Error: Binary not found in package" >&2
    exit 1
  fi

  chmod +x "$BIN_SRC"

  # Determine install directory
  INSTALL_DIR=""
  if [ -w "/usr/local/bin" ]; then
    INSTALL_DIR="/usr/local/bin"
  else
    INSTALL_DIR="$HOME/.local/bin"
    mkdir -p "$INSTALL_DIR"
  fi

  cp "$BIN_SRC" "$INSTALL_DIR/${BIN_NAME}"
  echo "Installed to ${INSTALL_DIR}/${BIN_NAME}"
}

print_success() {
  echo ""
  echo "swe-swe v${VERSION} installed successfully!"

  # Check if install dir is in PATH
  case ":$PATH:" in
    *":${INSTALL_DIR}:"*) ;;
    *)
      echo ""
      echo "Add ${INSTALL_DIR} to your PATH:"
      echo "  export PATH=\"${INSTALL_DIR}:\$PATH\""
      echo ""
      echo "To make it permanent, add the line above to your ~/.bashrc or ~/.zshrc"
      ;;
  esac
}

main
