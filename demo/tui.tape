# gooseneck TUI demo -> demo.gif (repo root)
# Render from the repo root:  demo/setup.sh && vhs demo/tui.tape
# Everything runs against the isolated /tmp/goose-demo home; setup is hidden
# so no prompt, username or path ever appears in the recording.
Output demo.gif

Set Shell "bash"
Set FontFamily "JetBrainsMono Nerd Font"
Set FontSize 15
Set Width 1440
Set Height 860
Set Padding 0
Set Framerate 24
Set TypingSpeed 70ms
Set CursorBlink false
Set Theme { "name": "gooseneck", "background": "#0a0a0a", "foreground": "#eeeeee", "cursor": "#fab283", "selection": "#1e1e1e", "black": "#0a0a0a", "red": "#e06c75", "green": "#7fd88f", "yellow": "#f5a742", "blue": "#5c9cf5", "magenta": "#9d7cd8", "cyan": "#56b6c2", "white": "#eeeeee", "brightBlack": "#808080", "brightRed": "#e06c75", "brightGreen": "#7fd88f", "brightYellow": "#f5a742", "brightBlue": "#5c9cf5", "brightMagenta": "#9d7cd8", "brightCyan": "#56b6c2", "brightWhite": "#ffffff" }

Hide
Type "export GOOSENECK_HOME=/tmp/goose-demo PATH=/tmp/goose-demo/bin:$PATH PS1='$ ' && cd /tmp && clear && goose"
Enter
Sleep 2s
Show

# home: logo + search-as-you-type
Sleep 2s
Type@120ms "8th"
Sleep 1.2s
Enter
Sleep 2.5s

# artist page: albums grid, move to an album and open it
Right@400ms 2
Left@400ms 2
Sleep 600ms
Enter
Sleep 2.5s

# play from track 2 -> sidebar comes alive
Down@350ms 1
Enter
Sleep 6s

# transport: pause, resume, next
Space
Sleep 1.5s
Space
Sleep 1s
Type "n"
Sleep 3s

# download the album for offline play (progress in the status bar)
Type "D"
Sleep 7s

# back to the artist, flip through sections
Escape
Sleep 1s
Tab
Sleep 1.4s
Tab
Sleep 1.4s
Tab
Sleep 1.6s

# the library with tag chips
Type "2"
Sleep 3s
Tab
Sleep 3s
Down@450ms 2
Right@450ms 2
Sleep 2s

# command palette -> theme picker with live preview
Ctrl+P
Sleep 800ms
Type@90ms "theme"
Sleep 600ms
Enter
Sleep 800ms
Down@700ms 3
Sleep 800ms
Up@700ms 3
Escape
Sleep 800ms

# queue, then the offline downloads page
Type "3"
Sleep 2.5s
Type "5"
Sleep 2.5s

# help
Type "?"
Sleep 2.5s
Escape
Sleep 800ms

Hide
Type "q"
Sleep 300ms
Type "goose quit-player; clear"
Enter
