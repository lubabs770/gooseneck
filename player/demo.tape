# gooseneck-player demo. Render with:  vhs demo.tape   (from this dir, after `go build -o gooseneck-player .`)
# Writes the GIF to the repo root so it doesn't trip the player/** CI path filter.
Output ../demo.gif

Set Shell "bash"
Set FontSize 18
Set Width 1200
Set Height 700
Set Padding 20
Set Theme "Catppuccin Mocha"
Set TypingSpeed 55ms
Set CursorBlink false

# launch (hidden so no shell prompt / username shows)
Hide
Type "./gooseneck-player"
Enter
Sleep 2s
Show

# grid view + vim navigation
Sleep 1s
Down@280ms 4
Right@280ms 3
Up@280ms 2
Sleep 600ms

# list view (caret follows the selection)
Type "v"
Sleep 1s
Down@250ms 4
Sleep 500ms
Type "v"
Sleep 700ms

# cycle themes
Type "t"
Sleep 800ms
Type "t"
Sleep 800ms
Type "t"
Sleep 900ms

# toggle the help line
Type "?"
Sleep 900ms
Type "?"
Sleep 900ms

# fuzzy filter (fzf-style)
Type "/"
Type "Avi Perets"
Sleep 1200ms
Enter
Sleep 800ms

# drill: artist -> albums -> tracks (served from cache, instant)
Enter
Sleep 1600ms
Enter
Sleep 1800ms
Backspace
Sleep 700ms
Backspace
Sleep 900ms

# live album indexing with progress bar (uncached artist)
Type "/"
Type "Yehuda Cohen"
Sleep 1000ms
Enter
Sleep 500ms
Enter
Sleep 25s

# indexed -> albums; drill into tracks, hold, then quit (hidden so no shell shows)
Enter
Sleep 2500ms
Hide
Type "q"
