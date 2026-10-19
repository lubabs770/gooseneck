package cli

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
	"github.com/spf13/cobra"
)

func (a *app) playbackCmds() []*cobra.Command {
	simple := func(use, short string, aliases []string, do func(l *library.Lib) (string, error)) *cobra.Command {
		return &cobra.Command{
			Use: use, Short: short, Aliases: aliases, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				l, err := a.lib()
				if err != nil {
					return err
				}
				msg, err := do(l)
				if err != nil {
					return err
				}
				return a.report(cmd, l, msg)
			},
		}
	}
	return []*cobra.Command{
		a.playCmd(),
		a.nowCmd(),
		a.queueCmd(),
		simple("pause", "Pause playback", nil, func(l *library.Lib) (string, error) {
			return "paused", l.Player.SetPause(true)
		}),
		simple("resume", "Resume playback", []string{"unpause"}, func(l *library.Lib) (string, error) {
			return "resumed", l.Player.SetPause(false)
		}),
		simple("toggle", "Toggle pause", []string{"pp"}, func(l *library.Lib) (string, error) {
			paused, err := l.Player.TogglePause()
			if paused {
				return "paused", err
			}
			return "playing", err
		}),
		simple("next", "Skip to the next track", []string{"n", "skip"}, func(l *library.Lib) (string, error) {
			return "", l.Player.Next()
		}),
		simple("prev", "Previous track (restarts the current one if >3s in)", []string{"previous", "back"}, func(l *library.Lib) (string, error) {
			return "", l.Player.Prev()
		}),
		simple("stop", "Stop playback and clear the queue", nil, func(l *library.Lib) (string, error) {
			return "stopped", l.Player.Stop()
		}),
		simple("quit-player", "Shut down the background player", nil, func(l *library.Lib) (string, error) {
			return "player shut down", l.Player.Quit()
		}),
		a.seekCmd(),
		a.volCmd(),
		a.repeatCmd(),
		simple("shuffle", "Shuffle the current queue", nil, func(l *library.Lib) (string, error) {
			return "queue shuffled", l.Player.Shuffle()
		}),
	}
}

// report prints a one-line confirmation followed by the now-playing line; with
// --json it prints the player status instead.
func (a *app) report(cmd *cobra.Command, l *library.Lib, msg string) error {
	p := a.out(cmd)
	time.Sleep(150 * time.Millisecond) // let mpv apply the change before reading it back
	np, err := l.Now()
	if p.json {
		if err != nil && !errors.Is(err, mpv.ErrNotRunning) {
			return err
		}
		return p.JSON(np)
	}
	if msg != "" {
		p.println(p.S(p.ok, "✓ ") + msg)
	}
	if err == nil && !np.Idle {
		printNow(p, np)
	}
	return nil
}

func (a *app) playCmd() *cobra.Command {
	var top, shuffle, add, next bool
	c := &cobra.Command{
		Use:     "play <artist> [album] [track]",
		Aliases: []string{"p"},
		Short:   "Play an artist, album or track (fuzzy names, numbers or YouTube URLs)",
		Long: `Play an artist, album or track.

  goose play <artist>                 everything they've released, in order
  goose play <artist> --top           just their top songs
  goose play <artist> <album>         an album (title, fragment, or its number in 'goose artist')
  goose play <artist> <album> <track> an album starting from a track (title or number)
  goose play <youtube url or id>      a single video

By default the queue is replaced. --add appends and --next inserts after the
current track.`,
		Example: "  goose play \"8th Day\" lucky\n  goose play fried --shuffle\n  goose play \"8th Day\" 1 3 --next",
		Args:    cobra.RangeArgs(1, 3),
		ValidArgsFunction: func(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
			return a.completeRelease(cmd, args, toComplete)
		},
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			mode := mpv.Replace
			switch {
			case add && next:
				return usage(errors.New("--add and --next can't be combined"))
			case add:
				mode = mpv.Append
			case next:
				mode = mpv.Next
			}
			req, label, err := a.buildPlay(cmd, l, args, top)
			if err != nil {
				return err
			}
			req.Mode, req.Shuffle = mode, shuffle
			if err := l.Play(req); err != nil {
				return err
			}
			verb := map[mpv.Mode]string{mpv.Replace: "Playing", mpv.Append: "Queued", mpv.Next: "Up next:"}[mode]
			if !l.Player.Managed() {
				verb = "Sent to " + l.Cfg.Player + ":"
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(map[string]any{"queued": len(req.Songs), "mode": strings.TrimSuffix(strings.ToLower(verb), ":"), "label": label})
			}
			suffix := ""
			if len(req.Songs) > 1 {
				suffix = fmt.Sprintf(" (%d tracks%s)", len(req.Songs), map[bool]string{true: ", shuffled"}[shuffle])
			}
			p.println(p.S(p.accent, "▶ ") + verb + " " + p.S(p.title, bidi.Vis(label)) + p.S(p.dim, suffix))
			return nil
		},
	}
	c.Flags().BoolVar(&top, "top", false, "only the artist's top songs")
	c.Flags().BoolVarP(&shuffle, "shuffle", "s", false, "shuffle before queueing")
	c.Flags().BoolVarP(&add, "add", "a", false, "append to the queue instead of replacing it")
	c.Flags().BoolVarP(&next, "next", "N", false, "insert after the current track")
	return c
}

// buildPlay resolves CLI arguments into songs plus a label for the output.
func (a *app) buildPlay(cmd *cobra.Command, l *library.Lib, args []string, top bool) (library.PlayRequest, string, error) {
	ctx := a.ctx(cmd)
	if len(args) == 1 {
		if id := mpv.VideoIDFromURL(args[0]); id != "" || looksLikeVideoID(args[0]) {
			if id == "" {
				id = args[0]
			}
			title := id
			if t, ok := l.DB.Track(id); ok {
				title = t.Title
			}
			return library.PlayRequest{Songs: []ytm.Song{{VideoID: id, Title: title}}}, title, nil
		}
	}
	ar, err := l.ResolveArtist(args[0])
	if err != nil {
		return library.PlayRequest{}, "", err
	}
	page, err := l.Artist(ctx, ar.ID, false)
	if err != nil {
		return library.PlayRequest{}, "", err
	}
	req := library.PlayRequest{ArtistID: page.ID, Artist: page.Name}
	if len(args) == 1 {
		if top {
			req.Songs = page.TopSongs
			return req, page.Name + " — top songs", nil
		}
		req.Songs, err = l.ArtistTracks(ctx, page)
		return req, page.Name, err
	}
	rel, err := library.ResolveRelease(page, args[1])
	if err != nil {
		return req, "", usage(err)
	}
	al, err := l.Album(ctx, rel.BrowseID, false)
	if err != nil {
		return req, "", err
	}
	req.Album = al.Title
	req.Songs = al.Tracks
	label := al.Title + " — " + page.Name
	if len(args) == 3 {
		i, err := library.ResolveTrack(al.Tracks, args[2])
		if err != nil {
			return req, "", usage(err)
		}
		req.Songs = al.Tracks[i:]
		label = al.Tracks[i].Title + " — " + page.Name
	}
	return req, label, nil
}

func looksLikeVideoID(s string) bool {
	if len(s) != 11 || strings.Contains(s, " ") {
		return false
	}
	for _, r := range s {
		if !(r == '-' || r == '_' || r >= '0' && r <= '9' || r >= 'a' && r <= 'z' || r >= 'A' && r <= 'Z') {
			return false
		}
	}
	// Require a digit, dash, underscore or mixed case so 11-letter words still
	// resolve as artist names.
	return strings.ContainsAny(s, "0123456789-_") || strings.ToLower(s) != s && strings.ToUpper(s) != s
}

func printNow(p *printer, np library.NowPlaying) {
	icon := "▶"
	if np.Paused {
		icon = "⏸"
	}
	t := np.Track
	title := t.Title
	if title == "" {
		title = np.Title
	}
	line := p.S(p.accent, icon+" ") + p.S(p.title, bidi.Vis(title))
	if t.Artist != "" {
		line += p.S(p.dim, " — ") + bidi.Vis(t.Artist)
	}
	if t.Album != "" && t.Album != t.Title {
		line += p.S(p.dim, " · "+bidi.Vis(t.Album))
	}
	p.println(line)
	dur := np.Duration
	if dur <= 0 {
		dur = float64(t.Duration)
	}
	frac := 0.0
	if dur > 0 {
		frac = np.Pos / dur
	}
	w := 30
	if p.width > 80 {
		w = 40
	}
	extras := []string{fmt.Sprintf("%d/%d", np.Index+1, np.Count), fmt.Sprintf("vol %d%%", int(np.Volume))}
	if np.Repeat != "off" {
		extras = append(extras, "repeat "+np.Repeat)
	}
	p.println("  " + p.bar(frac, w) + " " + ytm.FormatDuration(int(np.Pos)) + p.S(p.dim, " / "+ytm.FormatDuration(int(dur))+"   "+strings.Join(extras, " · ")))
}

func (a *app) nowCmd() *cobra.Command {
	var watch bool
	var format string
	c := &cobra.Command{
		Use:     "now",
		Aliases: []string{"status", "np"},
		Short:   "Show what's playing",
		Example: "  goose now\n  goose now --watch\n  goose now --format '{title} — {artist}'   # for status bars",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			show := func() error {
				np, err := l.Now()
				if err != nil {
					if errors.Is(err, mpv.ErrNotRunning) && (format != "" || p.json) {
						if p.json {
							return p.JSON(np)
						}
						return nil // status bars want empty output, not an error
					}
					return err
				}
				switch {
				case p.json:
					return p.JSON(np)
				case format != "":
					p.println(formatNow(format, np))
				case np.Idle:
					p.println(p.S(p.dim, "nothing playing"))
				default:
					printNow(p, np)
				}
				return nil
			}
			if !watch {
				return show()
			}
			for {
				p.printf("\x1b[H\x1b[2J")
				if err := show(); err != nil && !errors.Is(err, mpv.ErrNotRunning) {
					return err
				}
				select {
				case <-cmd.Context().Done():
					return nil
				case <-time.After(time.Second):
				}
			}
		},
	}
	c.Flags().BoolVarP(&watch, "watch", "w", false, "refresh every second")
	c.Flags().StringVarP(&format, "format", "f", "", "template: {title} {artist} {album} {pos} {duration} {state} {volume} {index} {count}")
	return c
}

func formatNow(f string, np library.NowPlaying) string {
	if np.Idle {
		return ""
	}
	state := "playing"
	if np.Paused {
		state = "paused"
	}
	title := np.Track.Title
	if title == "" {
		title = np.Title
	}
	r := strings.NewReplacer(
		"{title}", title, "{artist}", np.Track.Artist, "{album}", np.Track.Album,
		"{pos}", ytm.FormatDuration(int(np.Pos)), "{duration}", ytm.FormatDuration(int(np.Duration)),
		"{state}", state, "{volume}", strconv.Itoa(int(np.Volume)),
		"{index}", strconv.Itoa(np.Index+1), "{count}", strconv.Itoa(np.Count),
	)
	return r.Replace(f)
}

func (a *app) queueCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "queue",
		Aliases: []string{"q"},
		Short:   "Show the play queue (subcommands: jump, rm, clear)",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			items, err := l.Queue()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(items)
			}
			if len(items) == 0 {
				p.println(p.S(p.dim, "queue is empty"))
				return nil
			}
			for _, it := range items {
				marker := p.S(p.dim, fmt.Sprintf("%3d  ", it.Index+1))
				title := it.Track.Title
				if it.Current {
					marker = p.S(p.accent, "  ▶  ")
					title = p.S(p.accent, bidi.Vis(title))
				}
				right := bidi.Vis(it.Track.Artist)
				if it.Track.Duration > 0 {
					right += "  " + ytm.FormatDuration(it.Track.Duration)
				}
				p.row(marker, title, p.S(p.dim, right))
			}
			return nil
		},
	}
	index := func(s string) (int, error) {
		n, err := strconv.Atoi(s)
		if err != nil || n < 1 {
			return 0, usage(fmt.Errorf("want a queue position (1, 2, …), got %q", s))
		}
		return n - 1, nil
	}
	c.AddCommand(
		&cobra.Command{
			Use: "jump <n>", Short: "Play queue entry n", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				l, err := a.lib()
				if err != nil {
					return err
				}
				i, err := index(args[0])
				if err != nil {
					return err
				}
				if err := l.Player.Jump(i); err != nil {
					return err
				}
				return a.report(cmd, l, "")
			},
		},
		&cobra.Command{
			Use: "rm <n>", Aliases: []string{"remove"}, Short: "Remove queue entry n", Args: cobra.ExactArgs(1),
			RunE: func(cmd *cobra.Command, args []string) error {
				l, err := a.lib()
				if err != nil {
					return err
				}
				i, err := index(args[0])
				if err != nil {
					return err
				}
				if err := l.Player.Remove(i); err != nil {
					return err
				}
				return a.report(cmd, l, "removed #"+args[0])
			},
		},
		&cobra.Command{
			Use: "clear", Short: "Remove everything except the current track", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				l, err := a.lib()
				if err != nil {
					return err
				}
				if err := l.Player.Clear(); err != nil {
					return err
				}
				return a.report(cmd, l, "queue cleared")
			},
		},
	)
	return c
}

func (a *app) seekCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "seek <+10|-10|1:23|90>",
		Short:              "Seek relative (+/-seconds) or to a position (m:ss or seconds)",
		Example:            "  goose seek +30\n  goose seek -10\n  goose seek 1:15",
		DisableFlagParsing: true, // "-10" is a value, not a flag
		RunE: func(cmd *cobra.Command, args []string) error {
			args = a.takeGlobalFlags(args)
			if len(args) != 1 {
				return usage(errors.New("seek takes one argument, e.g. +10, -10 or 1:23"))
			}
			l, err := a.lib()
			if err != nil {
				return err
			}
			s := args[0]
			rel := strings.HasPrefix(s, "+") || strings.HasPrefix(s, "-")
			sec := float64(ytm.ParseDuration(strings.TrimLeft(s, "+-")))
			if sec == 0 && strings.Trim(s, "+-0:") != "" {
				return usage(fmt.Errorf("can't parse %q as a time", s))
			}
			if strings.HasPrefix(s, "-") {
				sec = -sec
			}
			if err := l.Player.Seek(sec, !rel); err != nil {
				return err
			}
			return a.report(cmd, l, "")
		},
	}
}

func (a *app) volCmd() *cobra.Command {
	return &cobra.Command{
		Use:                "vol [n|+n|-n]",
		Aliases:            []string{"volume"},
		Short:              "Show or change the volume (0-130)",
		DisableFlagParsing: true, // "-10" is a value, not a flag
		RunE: func(cmd *cobra.Command, args []string) error {
			args = a.takeGlobalFlags(args)
			if len(args) > 1 {
				return usage(errors.New("vol takes at most one argument"))
			}
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if len(args) == 0 {
				st, err := l.Player.Status()
				if err != nil {
					return err
				}
				if p.json {
					return p.JSON(map[string]float64{"volume": st.Volume})
				}
				p.println(int(st.Volume))
				return nil
			}
			s := args[0]
			n, err := strconv.ParseFloat(strings.TrimPrefix(s, "+"), 64)
			if err != nil {
				return usage(fmt.Errorf("volume must be a number, got %q", s))
			}
			abs := !strings.HasPrefix(s, "+") && !strings.HasPrefix(s, "-")
			v, err := l.Player.Volume(n, abs)
			if err != nil {
				return err
			}
			if p.json {
				return p.JSON(map[string]float64{"volume": v})
			}
			p.println(p.S(p.ok, "✓ ") + fmt.Sprintf("volume %d%%", int(v)))
			return nil
		},
	}
}

func (a *app) repeatCmd() *cobra.Command {
	return &cobra.Command{
		Use:       "repeat <off|all|one>",
		Short:     "Set repeat mode",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"off", "all", "one"},
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			if err := l.Player.Repeat(args[0]); err != nil {
				return usage(err)
			}
			return a.report(cmd, l, "repeat "+args[0])
		},
	}
}

// takeGlobalFlags pulls --json / --no-color / --help out of args for commands
// that disable flag parsing so negative numbers work.
func (a *app) takeGlobalFlags(args []string) []string {
	var out []string
	for _, arg := range args {
		switch arg {
		case "--json":
			a.asJSON = true
		case "--no-color":
			a.noColor = true
		default:
			out = append(out, arg)
		}
	}
	return out
}
