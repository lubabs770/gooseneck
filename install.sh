#!/bin/sh
# gooseneck-player installer. Detects OS/arch and drops the matching binary from
# the latest GitHub release into a bin dir on your PATH.
#
#   curl -fsSL https://raw.githubusercontent.com/lubabs770/gooseneck/main/install.sh | sh
#
# Env overrides:
#   BIN_DIR=/usr/local/bin   install location (default: ~/.local/bin)
#   VERSION=v0.1.0           pin a release (default: latest)
set -eu

REPO="lubabs770/gooseneck"
APP="gooseneck-player"
BIN_DIR="${BIN_DIR:-$HOME/.local/bin}"
VERSION="${VERSION:-latest}"

os=$(uname -s)
case "$os" in
	Linux)  GOOS=linux ;;
	Darwin) GOOS=darwin ;;
	*) echo "unsupported OS: $os (Windows: grab the .exe from the releases page)" >&2; exit 1 ;;
esac

arch=$(uname -m)
case "$arch" in
	x86_64|amd64)  GOARCH=amd64 ;;
	aarch64|arm64) GOARCH=arm64 ;;
	*) echo "unsupported arch: $arch" >&2; exit 1 ;;
esac

asset="${APP}-${GOOS}-${GOARCH}"
if [ "$VERSION" = latest ]; then
	url="https://github.com/${REPO}/releases/latest/download/${asset}"
else
	url="https://github.com/${REPO}/releases/download/${VERSION}/${asset}"
fi

echo "Installing ${APP} (${GOOS}/${GOARCH}) -> ${BIN_DIR}/${APP}"
mkdir -p "$BIN_DIR"
tmp=$(mktemp)
trap 'rm -f "$tmp"' EXIT
if ! curl -fsSL "$url" -o "$tmp"; then
	echo "download failed: $url" >&2
	echo "no release yet? tag one:  git tag v0.1.0 && git push origin v0.1.0" >&2
	exit 1
fi
chmod +x "$tmp"
mv "$tmp" "$BIN_DIR/${APP}"
trap - EXIT

# short alias: `goose`
ln -sf "$BIN_DIR/${APP}" "$BIN_DIR/goose"

echo "Installed as '${APP}' and 'goose'. Runtime deps: yt-dlp + mpv."
case ":$PATH:" in
	*":$BIN_DIR:"*) : ;;
	*) echo "note: $BIN_DIR is not on your PATH — add it, e.g.:"
	   echo "  export PATH=\"$BIN_DIR:\$PATH\"" ;;
esac
