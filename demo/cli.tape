# gooseneck CLI demo -> cli.gif (repo root)
# Render from the repo root:  demo/setup.sh && vhs demo/cli.tape
# Runs against the isolated /tmp/goose-demo home with a neutral "$ " prompt.
Output cli.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 16
Set Width 1200
Set Height 720
Set Padding 24
Set Framerate 24
Set TypingSpeed 45ms
Set CursorBlink false
Set Theme { "name": "gooseneck", "background": "#0a0a0a", "foreground": "#eeeeee", "cursor": "#fab283", "selection": "#1e1e1e", "black": "#0a0a0a", "red": "#e06c75", "green": "#7fd88f", "yellow": "#f5a742", "blue": "#5c9cf5", "magenta": "#9d7cd8", "cyan": "#56b6c2", "white": "#eeeeee", "brightBlack": "#808080", "brightRed": "#e06c75", "brightGreen": "#7fd88f", "brightYellow": "#f5a742", "brightBlue": "#5c9cf5", "brightMagenta": "#9d7cd8", "brightCyan": "#56b6c2", "brightWhite": "#ffffff" }

Hide
Type "export GOOSENECK_HOME=/tmp/goose-demo PATH=/tmp/goose-demo/bin:$PATH PS1='$ ' && cd /tmp && clear"
Enter
Sleep 500ms
Show

Type "goose search fried -n 4"
Sleep 300ms
Enter
Sleep 1.8s

Type "goose album '8th Day' lucky"
Sleep 300ms
Enter
Sleep 2.5s

Type "clear; goose play '8th Day' lucky --shuffle"
Sleep 300ms
Enter
Sleep 4s

Type "goose now"
Enter
Sleep 2.5s

Type "goose next && goose vol 60"
Enter
Sleep 3.5s

Type "goose now --format '{state}: {title} · {artist} [{pos}/{duration}]'"
Enter
Sleep 2.5s

Type "clear; goose queue"
Enter
Sleep 2.5s

Type `goose artist '8th Day' --json | jq -r '.albums[:4][] | "\(.year)  \(.title)"'`
Enter
Sleep 3s

Type "clear; goose radio 'Avraham Fried' && goose now"
Enter
Sleep 6s

Type "clear; goose download '8th Day' 'Inner Flame'"
Enter
Sleep 9s

Type "goose history -n 3"
Enter
Sleep 3s

Hide
Type "goose quit-player; clear"
Enter
