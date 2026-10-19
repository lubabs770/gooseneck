package cli

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/art"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/tui"
	"github.com/spf13/cobra"
)

func ago(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return fmt.Sprintf("%dm ago", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh ago", int(d.Hours()))
	case d < 30*24*time.Hour:
		return fmt.Sprintf("%dd ago", int(d.Hours()/24))
	}
	return t.Format("2006-01-02")
}

func (a *app) historyCmd() *cobra.Command {
	var n int
	c := &cobra.Command{
		Use:     "history",
		Aliases: []string{"recent"},
		Short:   "Show recently played artists, albums and tracks",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			h, err := l.DB.History(n)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(h)
			}
			if len(h) == 0 {
				p.println(p.S(p.dim, "nothing played yet — try: goose play <artist>"))
				return nil
			}
			for _, e := range h {
				what := e.Artist
				switch {
				case e.Track != "":
					what = e.Track + p.S(p.dim, " · "+e.Artist)
				case e.Album != "":
					what = e.Album + p.S(p.dim, " · "+e.Artist)
				}
				right := ago(e.At)
				if e.Count > 1 {
					right = fmt.Sprintf("%d tracks · %s", e.Count, right)
				}
				p.row("", what, p.S(p.dim, right))
			}
			return nil
		},
	}
	c.Flags().IntVarP(&n, "limit", "n", 20, "how many entries")
	return c
}

func (a *app) topCmd() *cobra.Command {
	var n int
	c := &cobra.Command{
		Use:   "top",
		Short: "Your most played artists",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			top, err := l.DB.TopArtists(n)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(top)
			}
			for i, t := range top {
				p.row(p.S(p.dim, fmt.Sprintf("%3d  ", i+1)), t.Artist,
					p.S(p.dim, fmt.Sprintf("%d plays · %d tracks · %s", t.Plays, t.Tracks, ago(t.Last))))
			}
			return nil
		},
	}
	c.Flags().IntVarP(&n, "limit", "n", 10, "how many artists")
	return c
}

func (a *app) cacheCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "cache",
		Short: "Cache info (subcommand: clear)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			thumbs, size := dirStats(config.ThumbDir())
			if p.json {
				return p.JSON(map[string]any{"pages": l.DB.CacheCount(), "images": thumbs, "image_bytes": size, "db": config.DBPath()})
			}
			p.row("", "cached pages", fmt.Sprint(l.DB.CacheCount()))
			p.row("", "cached images", fmt.Sprintf("%d (%.1f MB)", thumbs, float64(size)/1e6))
			p.row("", "database", config.DBPath())
			return nil
		},
	}
	var images bool
	clear := &cobra.Command{
		Use:   "clear",
		Short: "Drop cached artist/album pages (history is kept)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			n, err := l.DB.ClearCache()
			if err != nil {
				return err
			}
			msg := fmt.Sprintf("cleared %d cached pages", n)
			if images {
				_ = os.RemoveAll(config.ThumbDir())
				msg += " and all images"
			}
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + msg)
			return nil
		},
	}
	clear.Flags().BoolVar(&images, "images", false, "also delete downloaded artist/album art")
	c.AddCommand(clear, a.warmCmd())
	return c
}

func (a *app) warmCmd() *cobra.Command {
	var tag string
	var artists []string
	c := &cobra.Command{
		Use:   "warm",
		Short: "Prefetch artist pictures (and, with --artist, their pages and album art)",
		Example: "  goose cache warm --tag famous\n" +
			"  goose cache warm --artist \"8th Day\" --artist fried",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			tg, err := catalog.FindTag(tag)
			if err != nil {
				return usage(err)
			}
			p := a.out(cmd)
			var urls []string
			pages := 0
			if len(artists) > 0 {
				for _, q := range artists {
					ar, err := l.ResolveArtist(q)
					if err != nil {
						return err
					}
					page, err := l.Artist(cmd.Context(), ar.ID, false)
					if err != nil {
						return err
					}
					pages++
					urls = append(urls, ar.Thumbnail, page.Thumb)
					for _, r := range page.Releases() {
						urls = append(urls, r.Thumb)
						if _, err := l.Album(cmd.Context(), r.BrowseID, false); err == nil {
							pages++
						}
					}
					for _, r := range page.Related {
						urls = append(urls, r.Thumb)
					}
				}
			} else {
				for _, ar := range l.Cat.Filter(tg) {
					urls = append(urls, ar.Thumbnail)
				}
			}
			fetched, failed := art.Warm(urls, func(done, total int) {
				if p.tty && !p.json {
					p.printf("\r\x1b[2K%s warming %d/%d", p.S(p.accent, "⠿"), done, total)
				}
			})
			if p.tty && !p.json {
				p.printf("\r\x1b[2K")
			}
			if p.json {
				return p.JSON(map[string]int{"images": fetched, "failed": failed, "pages": pages})
			}
			msg := fmt.Sprintf("%d new images", fetched)
			if pages > 0 {
				msg += fmt.Sprintf(", %d pages cached", pages)
			}
			if failed > 0 {
				msg += p.S(p.warn, fmt.Sprintf(" (%d failed)", failed))
			}
			p.println(p.S(p.ok, "✓ ") + msg)
			return nil
		},
	}
	tagFlag(c, &tag)
	c.Flags().StringArrayVar(&artists, "artist", nil, "warm one artist's page, albums and art (repeatable)")
	c.RegisterFlagCompletionFunc("artist", a.completeArtist)
	return c
}

// mprisInstalled looks for the mpv-mpris plugin where mpv auto-loads scripts.
func mprisInstalled() bool {
	home, _ := os.UserHomeDir()
	for _, dir := range []string{filepath.Join(home, ".config", "mpv", "scripts"), "/etc/mpv/scripts", "/usr/share/mpv/scripts", "/usr/lib/mpv-mpris"} {
		if _, err := os.Stat(filepath.Join(dir, "mpris.so")); err == nil {
			return true
		}
	}
	return false
}

func dirStats(dir string) (n int, size int64) {
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if info, err := e.Info(); err == nil && !e.IsDir() {
			n++
			size += info.Size()
		}
	}
	return
}

func (a *app) configCmd() *cobra.Command {
	keyComp := func(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
		if len(args) == 0 {
			return config.Keys(), cobra.ShellCompDirectiveNoFileComp
		}
		if args[0] == "theme" {
			return tui.ThemeNames(), cobra.ShellCompDirectiveNoFileComp
		}
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	c := &cobra.Command{
		Use:   "config",
		Short: "Show the config (subcommands: get, set, path, edit)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.cfgErr != nil {
				return a.cfgErr
			}
			p := a.out(cmd)
			if p.json {
				out := map[string]string{}
				for _, k := range config.Keys() {
					out[k], _ = a.cfg.Get(k)
					if config.Secret(k) {
						out[k] = config.Mask(out[k])
					}
				}
				return p.JSON(out)
			}
			for _, k := range config.Keys() {
				v, _ := a.cfg.Get(k)
				if config.Secret(k) {
					v = config.Mask(v)
				}
				if k == "theme" && tui.ResolveThemeName(v) != v {
					v += p.S(p.dim, " → "+tui.ResolveThemeName(v))
				}
				p.row("", fmt.Sprintf("%-13s", k), v)
			}
			p.println(p.S(p.dim, config.Path()))
			return nil
		},
	}
	c.AddCommand(
		&cobra.Command{
			Use: "path", Short: "Print the config file path", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				fmt.Fprintln(cmd.OutOrStdout(), config.Path())
				return nil
			},
		},
		&cobra.Command{
			Use: "get <key>", Short: "Print one setting", Args: cobra.ExactArgs(1), ValidArgsFunction: keyComp,
			RunE: func(cmd *cobra.Command, args []string) error {
				v, err := a.cfg.Get(args[0])
				if err != nil {
					return usage(err)
				}
				fmt.Fprintln(cmd.OutOrStdout(), v)
				return nil
			},
		},
		&cobra.Command{
			Use: "set <key> <value>", Short: "Change one setting", Args: cobra.ExactArgs(2), ValidArgsFunction: keyComp,
			DisableFlagParsing: true, // values like "--no-video" are data, not flags
			Example:            "  goose config set theme tokyonight\n  goose config set player_args '--no-video --volume-max=150'",
			RunE: func(cmd *cobra.Command, args []string) error {
				if a.cfgErr != nil {
					return a.cfgErr
				}
				if args[0] == "theme" && !tui.HasTheme(args[1]) {
					return usage(fmt.Errorf("unknown theme %q (themes: %s)", args[1], strings.Join(tui.ThemeNames(), ", ")))
				}
				if err := a.cfg.Set(args[0], args[1]); err != nil {
					return usage(err)
				}
				if err := config.Update(func(c *config.Config) error { return c.Set(args[0], args[1]) }); err != nil {
					return err
				}
				p := a.out(cmd)
				v, _ := a.cfg.Get(args[0])
				p.println(p.S(p.ok, "✓ ") + args[0] + " = " + v)
				return nil
			},
		},
		&cobra.Command{
			Use: "edit", Short: "Open the config in $EDITOR", Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				ed := os.Getenv("VISUAL")
				if ed == "" {
					ed = os.Getenv("EDITOR")
				}
				if ed == "" {
					ed = "vi"
				}
				e := exec.Command("sh", "-c", ed+` "$1"`, "sh", config.Path())
				e.Stdin, e.Stdout, e.Stderr = os.Stdin, os.Stdout, os.Stderr
				return e.Run()
			},
		},
	)
	return c
}

func (a *app) doctorCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Check that everything gooseneck needs is installed and reachable",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.out(cmd)
			type check struct {
				Name   string `json:"name"`
				OK     bool   `json:"ok"`
				Detail string `json:"detail"`
				Fix    string `json:"fix,omitempty"`
			}
			var checks []check
			add := func(name string, ok bool, detail, fix string) {
				checks = append(checks, check{name, ok, detail, fix})
			}

			if a.cfgErr != nil {
				add("config", false, a.cfgErr.Error(), "fix or delete "+config.Path())
			} else {
				add("config", true, config.Path(), "")
			}
			l, err := a.lib()
			if err != nil {
				add("catalog", false, err.Error(), "goose catalog update")
			} else {
				age := time.Since(l.Cat.ModTime)
				add("catalog", age < 14*24*time.Hour, fmt.Sprintf("%d artists, updated %s", l.Cat.Len(), ago(l.Cat.ModTime)),
					map[bool]string{true: "goose catalog update"}[age >= 14*24*time.Hour])
				if v := l.YtDlpVersion(); v != "" {
					add("yt-dlp", true, v, "")
				} else {
					add("yt-dlp", false, "not found", "install yt-dlp (https://github.com/yt-dlp/yt-dlp) or set bin_dir")
				}
				if path, err := exec.LookPath(a.cfg.Player); err == nil {
					out, _ := exec.Command(path, "--version").Output()
					first := strings.SplitN(string(out), "\n", 2)[0]
					add("player", true, strings.TrimSpace(first), "")
				} else {
					add("player", false, a.cfg.Player+" not found", "install mpv (https://mpv.io)")
				}
				switch {
				case !l.Player.Managed():
					add("control", false, "playback control needs mpv on Linux/macOS", "")
				case l.Player.Alive():
					add("control", true, "player running · "+l.Player.Sock, "")
				default:
					add("control", true, "player idle (starts on first play)", "")
				}
				ctx, cancel := context.WithTimeout(cmd.Context(), 10*time.Second)
				start := time.Now()
				_, err := l.YTM.Artist(ctx, "UCw1oCoEMF_o65tpDHItwwUQ")
				cancel()
				if err != nil {
					add("youtube music", false, err.Error(), "check your network; cached pages still work offline")
				} else {
					add("youtube music", true, fmt.Sprintf("reachable (%dms)", time.Since(start).Milliseconds()), "")
				}
			}
			if svcs := l.ScrobbleServices(); len(svcs) > 0 {
				var names []string
				for _, s := range svcs {
					names = append(names, s.Name())
				}
				detail := "to " + strings.Join(names, " + ")
				if _, err := library.QueryScrobbler(); err == nil {
					detail += " · scrobbler running"
				}
				add("scrobbling", true, detail, "")
			} else {
				add("scrobbling", true, "optional: goose scrobble login listenbrainz|lastfm", "")
			}
			if runtime.GOOS == "linux" {
				if mprisInstalled() {
					add("media keys", true, "mpv-mpris found (desktop media controls work)", "")
				} else {
					add("media keys", true, "optional: install mpv-mpris for media keys and desktop widgets", "")
				}
			}
			img := "half-block art (any truecolor terminal)"
			if tui.KittyCapable() {
				img = "kitty graphics (real images)"
			}
			add("images", true, img, "")

			if p.json {
				return p.JSON(checks)
			}
			bad := 0
			for _, c := range checks {
				mark := p.S(p.ok, "✓")
				if !c.OK {
					mark = p.S(p.bad, "✗")
					bad++
				}
				p.row(mark+" ", fmt.Sprintf("%-14s", c.Name)+c.Detail, "")
				if c.Fix != "" {
					p.println("    " + p.S(p.dim, "→ "+c.Fix))
				}
			}
			if bad > 0 {
				return &exitError{1, fmt.Errorf("%d check(s) failed", bad)}
			}
			return nil
		},
	}
}
