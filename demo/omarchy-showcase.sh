#!/usr/bin/env bash
# Drives a live goose showcase on an Omarchy desktop, for screen recording.
#
#   demo/omarchy-showcase.sh               # full run (~4 min), records itself
#   SPEED=0.7 demo/omarchy-showcase.sh     # faster (multiplies every pause)
#   THEMES="Nord|Kanagawa" demo/omarchy-showcase.sh
#   RECORD=0 demo/omarchy-showcase.sh      # don't record (record it yourself)
#   DRIVER=tmux demo/omarchy-showcase.sh   # dry run, see below
#
# Desktop run: switches to an empty workspace, starts a fullscreen recording
# with desktop audio (Omarchy's recorder, saved to ~/Videos), opens goose from
# the Omarchy apps menu, tours the TUI with real key presses (wtype), flips
# Omarchy themes while music plays so goose restyles live, then shows the CLI
# driving the same player from a terminal. Theme and workspace are restored at
# the end, and on Ctrl+C (one press is enough; cleanup can't be interrupted).
#
# Clean by default: goose runs on a separate demo home (no real history), the
# Goose menu entry is pointed at it for the run, and the CLI terminal uses the
# robbyrussell prompt (no user@host) by swapping the two theme lines in
# ~/.zshrc. All of that is put back afterwards. CLEAN=0 uses your real setup.
# It plays real audio. Hands off the keyboard and
# mouse while it runs: keys go to the focused window (goose is refocused before
# every key in case something steals focus).
#
# Dry run (DRIVER=tmux): the same key sequence goes to goose in a hidden tmux
# session, with a throwaway goose home and silent playback. Nothing on the
# desktop changes. A text snapshot of the screen is saved after every step to
# $TEST_DIR/snaps.txt, so the whole tour can be checked without watching it.
set -uo pipefail

SPEED=${SPEED:-1}
ARTIST=${ARTIST:-"8th Day"}
RECORD=${RECORD:-1}
THEMES=${THEMES:-"Tokyo Night|Gruvbox|Catppuccin Latte|Rose Pine|Kanagawa"}
DRIVER=${DRIVER:-desktop}
TEST_DIR=${TEST_DIR:-/tmp/goose-showcase-test}
CLASS=TUI.float # set by ~/.local/share/applications/Goose.desktop
TMUX_SESSION=goose-showcase
CLEAN=${CLEAN:-1}
DEMO_HOME=${DEMO_HOME:-${XDG_CACHE_HOME:-$HOME/.cache}/goose-showcase}
DESKTOP_FILE=$HOME/.local/share/applications/Goose.desktop
ZSHRC=$HOME/.zshrc
THEME_ON='source ~/.zsh/themes/aphrodite/aphrodite.zsh-theme'
THEME_DEMO='source ~/.zsh/themes/robbyrussell/robbyrussell.sh'

case $DRIVER in
desktop) need=(wtype hyprctl jq goose omarchy omarchy-menu) ;;
tmux) need=(tmux jq goose) ;;
*) echo "DRIVER must be desktop or tmux" >&2; exit 1 ;;
esac
for cmd in "${need[@]}"; do
	command -v "$cmd" >/dev/null || { echo "missing: $cmd" >&2; exit 1; }
done

# ---------- helpers ----------

pause() { sleep "$(awk -v a="$1" -v s="$SPEED" 'BEGIN{print a*s}')"; }

# hl <lua dispatcher> — Hyprland 0.56 Lua dispatch
hl() { hyprctl dispatch "$1" >/dev/null 2>&1; }

goose_addr() {
	hyprctl clients -j | jq -r --arg c "$CLASS" '[.[] | select(.class == $c)][0].address // empty'
}

# Keep keys going to goose even if a notification or a click stole focus.
focus_goose() {
	local addr
	addr=$(goose_addr)
	[[ -z $addr ]] && return
	[[ $(hyprctl activewindow -j | jq -r .address) == "$addr" ]] && return
	hl "hl.dsp.focus({ window = \"address:$addr\" })"
	sleep 0.2
}

# wtype key names → tmux key names
tmux_key() {
	case $1 in
	Return) echo Enter ;;
	space) echo Space ;;
	bracketright) echo ']' ;;
	minus) echo - ;;
	equal) echo = ;;
	question) echo '?' ;;
	*) echo "$1" ;;
	esac
}

send_key() {
	if [[ $DRIVER == tmux ]]; then
		tmux send-keys -t "$TMUX_SESSION" "$(tmux_key "$1")"
	else
		focus_goose
		wtype -k "$1"
	fi
}

# key <name> [times] [gap] — a named key (Return, Tab, Escape, Down, space…)
key() {
	local k=$1 n=${2:-1} gap=${3:-0.45} i
	for ((i = 0; i < n; i++)); do
		send_key "$k"
		pause "$gap"
	done
}

# shift_tab [times] [gap]
shift_tab() {
	local n=${1:-1} gap=${2:-0.6} i
	for ((i = 0; i < n; i++)); do
		if [[ $DRIVER == tmux ]]; then
			tmux send-keys -t "$TMUX_SESSION" BTab
		else
			focus_goose
			wtype -M shift -k Tab -m shift
		fi
		pause "$gap"
	done
}

# typ <text> — typed like a person would
typ() {
	if [[ $DRIVER == tmux ]]; then
		local i
		for ((i = 0; i < ${#1}; i++)); do
			tmux send-keys -t "$TMUX_SESSION" -l -- "${1:i:1}"
			sleep 0.11
		done
	else
		focus_goose
		wtype -d 110 -- "$1"
	fi
}

# ctrl <letter>
ctrl() {
	if [[ $DRIVER == tmux ]]; then
		tmux send-keys -t "$TMUX_SESSION" "C-$1"
	else
		focus_goose
		wtype -M ctrl -k "$1" -m ctrl
	fi
}

notify() { echo "  · $*"; }

# snap <label> — dry run only: save what the screen shows right now
snap() {
	[[ $DRIVER == tmux ]] || return 0
	{
		printf '\n===== %s =====\n' "$1"
		tmux capture-pane -p -t "$TMUX_SESSION"
	} >>"$TEST_DIR/snaps.txt"
}

# step <label> — announce an act and snapshot the screen it starts from
step() { notify "$1"; snap "before: $1"; }

# Wait until the player has really started (yt-dlp can take a few seconds to
# resolve the first track), so the transport keys never fire into a spinner.
wait_playing() {
	local i
	for ((i = 0; i < 60; i++)); do
		goose now --json 2>/dev/null | jq -e '.running and .count > 0 and .position > 0.5' >/dev/null && return 0
		sleep 0.5
	done
	echo "  ! playback didn't start within 30s; carrying on" >&2
}

theme() {
	notify "Omarchy theme → $1"
	[[ $DRIVER == tmux ]] && return 0
	omarchy theme set "$1" >/dev/null 2>&1
}

recording=0
start_recording() {
	[[ $RECORD == 1 && $DRIVER == desktop ]] || return 0
	if pgrep -f '^gpu-screen-recorder' >/dev/null; then
		echo "a screen recording is already running; not starting another" >&2
		return 0
	fi
	omarchy capture screenrecording --fullscreen --with-desktop-audio >/dev/null 2>&1
	if pgrep -f '^gpu-screen-recorder' >/dev/null; then
		recording=1
	else
		echo "screen recording failed to start; carrying on without it" >&2
	fi
}

stop_recording() {
	((recording)) || return 0
	recording=0
	local file
	file=$(omarchy capture screenrecording --stop-recording 2>/dev/null | tail -1)
	echo "saved recording: ${file:-~/Videos}"
}

# Clean setup: point the Goose menu entry at the demo home and switch the zsh
# prompt to robbyrussell (comment one theme line, uncomment the other).
swapped_desktop=0 swapped_zsh=0
clean_setup() {
	[[ $CLEAN == 1 && $DRIVER == desktop ]] || return 0
	cp "$DESKTOP_FILE" "$DEMO_HOME/Goose.desktop.orig"
	sed -i "s|^Exec=|Exec=env GOOSENECK_HOME=$DEMO_HOME |" "$DESKTOP_FILE"
	swapped_desktop=1
	if grep -q "^\s*$THEME_ON" "$ZSHRC" && grep -q "^\s*# $THEME_DEMO" "$ZSHRC"; then
		sed -i -E "s|^(\s*)$THEME_ON|\1# $THEME_ON|; s|^(\s*)# $THEME_DEMO|\1$THEME_DEMO|" "$ZSHRC"
		swapped_zsh=1
	else
		echo "  ! zshrc theme lines not as expected; leaving the prompt alone" >&2
	fi
	sleep 1 # let the menu notice the edited entry
}

clean_restore() {
	if ((swapped_desktop)); then
		cp "$DEMO_HOME/Goose.desktop.orig" "$DESKTOP_FILE"
		swapped_desktop=0
	fi
	if ((swapped_zsh)); then
		sed -i -E "s|^(\s*)$THEME_DEMO|\1# $THEME_DEMO|; s|^(\s*)# $THEME_ON|\1$THEME_ON|" "$ZSHRC"
		swapped_zsh=0
	fi
	[[ $CLEAN == 1 ]] && goose quit-player >/dev/null 2>&1
}

cleanup() {
	trap '' INT TERM # a second Ctrl+C must not cut the restore short
	trap - EXIT
	echo
	echo "cleaning up…"
	if [[ $DRIVER == tmux ]]; then
		goose quit-player >/dev/null 2>&1
		tmux kill-session -t "$TMUX_SESSION" 2>/dev/null
		echo "snapshots: $TEST_DIR/snaps.txt"
		return
	fi
	stop_recording
	clean_restore
	echo "restoring theme '$orig_theme' and workspace $orig_ws"
	[[ $(omarchy theme current) != "$orig_theme" ]] && omarchy theme set "$orig_theme" >/dev/null 2>&1
	hl "hl.dsp.focus({ workspace = \"$orig_ws\" })"
}
on_signal() { cleanup; exit 130; }

# ---------- prep (not recorded) ----------

if [[ $DRIVER == tmux ]]; then
	# Throwaway goose home: own config, cache, history and player socket.
	export GOOSENECK_HOME=$TEST_DIR/home
	mkdir -p "$GOOSENECK_HOME"
	: >"$TEST_DIR/snaps.txt"
	goose config set player_args "--no-video --ao=null" >/dev/null
	goose config set theme opencode >/dev/null
	tmux kill-session -t "$TMUX_SESSION" 2>/dev/null
else
	if [[ $CLEAN == 1 ]]; then
		export GOOSENECK_HOME=$DEMO_HOME
		mkdir -p "$GOOSENECK_HOME"
	fi
	orig_theme=$(omarchy theme current)
	orig_ws=$(hyprctl activeworkspace -j | jq -r .id)
fi
trap cleanup EXIT
trap on_signal INT TERM
clean_setup

echo "warming pages and art (slow the first time, instant after)…"
goose cache warm --tag famous >/dev/null 2>&1 || true
goose cache warm --artist "$ARTIST" >/dev/null 2>&1 || true
goose stop >/dev/null 2>&1 || true

if [[ $DRIVER == desktop ]]; then
	echo
	if [[ $RECORD == 1 ]]; then
		echo "Hands off the keyboard and mouse. Recording starts in 3s; Ctrl+C aborts."
		n=3
	else
		echo "Start your screen recording now. Showcase begins in 6s; Ctrl+C aborts."
		n=6
	fi
	for ((i = n; i > 0; i--)); do printf '\r  %s ' "$i"; sleep 1; done
	echo

	# An empty workspace makes a clean stage.
	hl 'hl.dsp.focus({ workspace = "empty" })'
	pause 0.8
	start_recording
	pause 1.5
fi

# ---------- act 1: launch from the Omarchy apps menu ----------

step "launch goose from the Omarchy apps menu"
if [[ $DRIVER == tmux ]]; then
	tmux new-session -d -s "$TMUX_SESSION" -x 160 -y 44 \
		"GOOSENECK_HOME=$GOOSENECK_HOME goose tui library --tab all"
	pause 3
else
	omarchy-menu toggle apps &
	pause 1.5
	wtype -d 140 goose
	pause 1.2
	wtype -k Return

	for _ in $(seq 50); do [[ -n $(goose_addr) ]] && break; sleep 0.2; done
	[[ -z $(goose_addr) ]] && { echo "goose window never appeared" >&2; exit 1; }
	pause 1
	# Give the floating window a recording-friendly size.
	read -r mw mh < <(hyprctl monitors -j | jq -r '.[] | select(.focused) | "\(.width / .scale | floor) \(.height / .scale | floor)"')
	hl "hl.dsp.window.resize({ x = $((mw * 85 / 100)), y = $((mh * 85 / 100)) })"
	hl 'hl.dsp.window.center()'
	pause 3
fi

# ---------- act 2: the library (opens on the "All" tag) ----------

step "library: grid, tag chips, list view"
key Right 3 0.35
key Down 2 0.4
pause 1
key Tab 1 1.8 # Popular
key Tab 1 1.8 # Chasidish
key Tab 1 1.8 # Israeli
snap "library on the Israeli tag"
key v 1 2.2 # list view
key Down 4 0.3
snap "library list view"
key v 1 1.5 # back to grid
shift_tab 3 0.6 # back to All, so the filter searches everyone

step "library: filter"
typ /
typ fried
pause 2
snap "library filtered to 'fried'"
key Escape 1 1.2 # one Esc clears the filter and leaves the search box

# ---------- act 3: home search → artist → album → play ----------

# "1" goes home, and home puts the cursor straight in its search box, so the
# next thing typed is the search.
step "home: search as you type"
key 1 1 1.5
typ "${ARTIST%% *}" # first word is enough for the fuzzy search
pause 1.5
snap "home search results"
key Return 1 0.4
# goose bug (v0.8.0): the home search text carries over as the artist page's
# filter ("8th" leaves only "8th Day Live"). Esc clears it.
key Escape 1 2.6

step "artist page sections"
key Tab 1 1.6 # singles
key Tab 1 1.6 # songs
key Tab 1 1.8 # related
snap "artist: related"
key Tab 1 1.4 # back to albums
key Right 1 0.4
key Left 1 0.6
key Return 1 3 # open the first album

step "play from track 2"
key Down 1 0.6
key Return 1 0.5
wait_playing
pause 3
snap "playing"

# ---------- act 4: transport ----------

step "transport: pause, skip, seek, volume, repeat"
key space 1 1.6 # pause
snap "paused"
key space 1 1.2 # resume
key n 1 0.5 # next track
wait_playing
pause 2
key bracketright 3 0.6 # seek +10s ×3
key minus 2 0.5 # volume down
key equal 2 1 # and back up
key r 1 1.2 # repeat all
key r 1 1.2 # repeat one
key r 1 1.2 # off
snap "after transport"

# ---------- act 5: Omarchy theme changes, goose follows live ----------

step "Omarchy themes while music plays"
IFS='|' read -ra theme_list <<<"$THEMES"
for t in "${theme_list[@]}"; do
	theme "$t"
	pause 4
done

# ---------- act 6: goose's own theme picker (live preview, then cancel) ----------

step "in-app theme picker"
key t 1 1.2
key Down 5 0.9
key Up 2 0.9
snap "theme picker"
key Escape 1 1.2 # cancel → back to the Omarchy colors

# ---------- act 7: layout toggles ----------

step "sidebar and art toggles"
key b 1 1.8 # hide sidebar
snap "sidebar hidden"
key b 1 1.5
key i 1 1.8 # art off
key i 1 1.5

# ---------- act 8: command palette → endless radio ----------

step "command palette: radio"
ctrl p
pause 1
typ radio
pause 1
snap "palette"
key Return 1 3

# ---------- act 9: queue, history, downloads ----------

step "queue, history, downloads"
key 3 1 2.5 # queue (radio filled it)
key Down 4 0.35
snap "queue"
key 4 1 2.2 # history
key Tab 1 2 # top
key Tab 1 2.2 # listens
snap "history: listens"
key 5 1 2.2 # downloads
snap "downloads"

# ---------- act 10: help, then quit (music keeps playing) ----------

step "help"
key question 1 3
snap "help"
key Escape 1 1

# No "1" here: home would put the cursor in its search box and the q below
# would be typed into it instead of quitting.
step "quit the TUI — music keeps going"
key q 1 1.5

# ---------- act 11: the CLI controls the same player ----------

step "CLI in a terminal"
cli=(
	"clear|0.5"
	"goose now|3"
	"goose queue | head -8|3"
	"goose next|1.5"
	"goose now|3"
	"goose search shwekey|3"
	"goose artist '$ARTIST' | head -14|4"
	"goose top|3"
	"goose tags|3"
	"goose stop|1.5"
)
if [[ $DRIVER == tmux ]]; then
	for entry in "${cli[@]}"; do
		c=${entry%|*}
		[[ $c == clear ]] && continue
		{ printf '\n===== $ %s =====\n' "$c"; bash -c "$c" 2>&1; } >>"$TEST_DIR/snaps.txt"
	done
else
	# Started from here so it inherits GOOSENECK_HOME (the demo home).
	if command -v foot >/dev/null; then
		setsid -f foot >/dev/null 2>&1
	else
		omarchy launch terminal >/dev/null 2>&1 &
	fi
	for _ in $(seq 50); do
		[[ $(hyprctl activewindow -j | jq -r .class) =~ foot|ghostty|alacritty|kitty ]] && break
		sleep 0.2
	done
	pause 1.5
	for entry in "${cli[@]}"; do
		wtype -d 70 -- "${entry%|*}"
		pause 0.4
		wtype -k Return
		pause "${entry##*|}"
	done
fi

notify "done"
pause 2
