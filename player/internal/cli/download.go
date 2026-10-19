package cli

import (
	"fmt"
	"sync"

	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/spf13/cobra"
)

func (a *app) downloadCmd() *cobra.Command {
	var top bool
	c := &cobra.Command{
		Use:     "download <artist> [album] [track]",
		Aliases: []string{"dl"},
		Short:   "Download an artist, album or track for offline play",
		Long: `Download audio with yt-dlp for offline play. Arguments work like 'goose play':

  goose download <artist>             everything they've released
  goose download <artist> --top       just their top songs
  goose download <artist> <album>     one album (title, fragment or number)
  goose download <artist> <album> <n> one track

Files go to download_dir (default ~/Music/gooseneck/<Artist>/<Album>/).
Downloaded tracks play from disk automatically; already-downloaded tracks are
skipped, so re-running a download only fetches what's new.`,
		Example:           "  goose download \"8th Day\" lucky\n  goose download fried --top",
		Args:              cobra.RangeArgs(1, 3),
		ValidArgsFunction: a.completeRelease,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			req, label, err := a.buildPlay(cmd, l, args, top)
			if err != nil {
				return err
			}
			if len(args) == 3 { // play resolves "from this track on"; download just that one
				req.Songs = req.Songs[:1]
			}
			p := a.out(cmd)
			if !p.json {
				p.println(p.S(p.accent, "↓ ") + "Downloading " + p.S(p.title, bidi.Vis(label)) +
					p.S(p.dim, fmt.Sprintf(" (%d tracks) → %s", len(req.Songs), l.Cfg.Downloads())))
			}
			var mu sync.Mutex
			var events []library.DownloadEvent
			res := l.Download(cmd.Context(), req.Songs, func(ev library.DownloadEvent) {
				mu.Lock()
				defer mu.Unlock()
				if p.json {
					if ev.Done {
						events = append(events, ev)
					}
					return
				}
				title := bidi.Vis(ev.Song.Title)
				switch {
				case !ev.Done:
					if p.tty {
						p.printf("\r\x1b[2K  %s %s %s", p.S(p.accent, fmt.Sprintf("%3.0f%%", ev.Percent)),
							truncateRunes(title, 50), p.S(p.dim, fmt.Sprintf("[%d/%d]", ev.Index+1, ev.Total)))
					}
					return
				case ev.Skipped:
					p.clearLine()
					p.println("  " + p.S(p.dim, "• "+title+" (already downloaded)"))
				case ev.Err != nil:
					p.clearLine()
					p.println("  " + p.S(p.bad, "✗ ") + title + p.S(p.dim, "  "+ev.Err.Error()))
				default:
					p.clearLine()
					p.println("  " + p.S(p.ok, "✓ ") + title)
				}
			})
			if p.json {
				return p.JSON(map[string]any{"downloaded": res.Downloaded, "skipped": res.Skipped,
					"failed": res.Failed, "bytes": res.Bytes, "dir": l.Cfg.Downloads(), "tracks": events})
			}
			summary := fmt.Sprintf("%d downloaded (%.1f MB)", res.Downloaded, float64(res.Bytes)/1e6)
			if res.Skipped > 0 {
				summary += fmt.Sprintf(", %d already there", res.Skipped)
			}
			if res.Failed > 0 {
				p.println(p.S(p.warn, "! ") + summary + p.S(p.bad, fmt.Sprintf(", %d failed", res.Failed)))
				return &exitError{1, fmt.Errorf("%d track(s) failed to download", res.Failed)}
			}
			p.println(p.S(p.ok, "✓ ") + summary)
			return nil
		},
	}
	c.Flags().BoolVar(&top, "top", false, "only the artist's top songs")
	return c
}

func (a *app) downloadsCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "downloads",
		Short: "List downloaded tracks (subcommand: rm)",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			list, err := l.DB.Downloads()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(list)
			}
			if len(list) == 0 {
				p.println(p.S(p.dim, "nothing downloaded yet — try: goose download <artist> <album>"))
				return nil
			}
			var total int64
			group := ""
			for _, d := range list {
				g := d.Artist + " · " + orDash(d.Album)
				if g != group {
					if group != "" {
						p.println()
					}
					p.println(p.S(p.accent, bidi.Vis(g)))
					group = g
				}
				total += d.Size
				p.row("  ", d.Title, p.S(p.dim, fmt.Sprintf("%.1f MB", float64(d.Size)/1e6)))
			}
			p.println()
			p.println(p.S(p.dim, fmt.Sprintf("%d tracks · %.1f MB · %s", len(list), float64(total)/1e6, l.Cfg.Downloads())))
			return nil
		},
	}
	c.AddCommand(&cobra.Command{
		Use:               "rm <artist> [album] [track]",
		Aliases:           []string{"remove"},
		Short:             "Delete downloaded files (same arguments as download)",
		Args:              cobra.RangeArgs(1, 3),
		ValidArgsFunction: a.completeRelease,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			req, label, err := a.buildPlay(cmd, l, args, false)
			if err != nil {
				return err
			}
			if len(args) == 3 {
				req.Songs = req.Songs[:1]
			}
			ids := make([]string, len(req.Songs))
			for i, s := range req.Songs {
				ids[i] = s.VideoID
			}
			n, err := l.RemoveDownloads(ids)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(map[string]int{"removed": n})
			}
			p.println(p.S(p.ok, "✓ ") + fmt.Sprintf("removed %d downloaded tracks of %s", n, bidi.Vis(label)))
			return nil
		},
	})
	return c
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func truncateRunes(s string, n int) string {
	r := []rune(s)
	if len(r) <= n {
		return s
	}
	return string(r[:n-1]) + "…"
}

func (p *printer) clearLine() {
	if p.tty {
		p.printf("\r\x1b[2K")
	}
}
