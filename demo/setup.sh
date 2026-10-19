#!/bin/sh
# Prepares everything the VHS tapes need, in an isolated home:
#   - builds goose into /tmp/goose-demo/bin
#   - fresh config, history and cache under /tmp/goose-demo (never yours)
#   - silent playback (--ao=null) so recording doesn't blast audio
#   - pre-warms pages and art so the GIFs don't sit on network spinners
#
# Usage (from the repo root):  demo/setup.sh && vhs demo/tui.tape && vhs demo/cli.tape
set -eu
root=$(cd "$(dirname "$0")/.." && pwd)
export GOOSENECK_HOME=/tmp/goose-demo
G="$GOOSENECK_HOME/bin/goose"

"$G" quit-player >/dev/null 2>&1 || true
rm -rf "$GOOSENECK_HOME"
mkdir -p "$GOOSENECK_HOME/bin"
(cd "$root/player" && go build -ldflags "-X main.version=$(git -C "$root" describe --tags --always 2>/dev/null || echo dev)" -o "$G" .)

cd /tmp # no repo-local artists.json: use the downloaded catalog like a real install
"$G" config set theme opencode >/dev/null
"$G" config set player_args "--no-video --ao=null" >/dev/null
"$G" catalog update >/dev/null
"$G" cache warm --tag famous >/dev/null
"$G" cache warm --artist "8th Day" --artist "Avraham Fried" --artist "Mordechai Ben David" \
	--artist "Shulem Lemmer" --artist "Benny Friedman" >/dev/null

# a little listening history so the home screen has "recently played"
for a in "Mordechai Ben David" "Avraham Fried" "Shulem Lemmer" "Benny Friedman"; do
	"$G" play "$a" --top >/dev/null
	sleep 1
done
"$G" quit-player >/dev/null
echo "demo home ready: $GOOSENECK_HOME"
