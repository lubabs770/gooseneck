package cli

import (
	"errors"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/lyrics"
	"github.com/spf13/cobra"
)

func (a *app) lyricsCmd() *cobra.Command {
	var follow, fresh bool
	c := &cobra.Command{
		Use:   "lyrics",
		Short: "Show the current track's lyrics (from lrclib.net)",
		Long: `Show the lyrics of what's playing, looked up on LRCLIB (lrclib.net).

With synced lyrics the line being sung is highlighted. --follow prints each
line as it's sung (one per line, for status bars); it keeps going across
tracks until interrupted.`,
		Example: "  goose lyrics\n  goose lyrics --follow",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			np, err := l.Now()
			if err != nil {
				return err
			}
			if np.Idle {
				return errors.New("nothing playing")
			}
			ly, err := l.Lyrics(cmd.Context(), np.Track, fresh)
			if err != nil {
				return err
			}
			lines := ly.Lines()
			if !follow {
				cur := lyrics.Current(lines, np.Pos)
				if p.json {
					return p.JSON(map[string]any{"track": np.Track, "lyrics": ly, "lines": lines, "current": cur})
				}
				printNow(p, np)
				p.println("")
				switch {
				case ly == nil:
					p.println(p.S(p.dim, "no lyrics found on lrclib.net"))
				case ly.Instrumental:
					p.println(p.S(p.dim, "♪ instrumental"))
				}
				for i, ln := range lines {
					if i == cur {
						p.println(p.S(p.accent, "› ") + p.S(p.title, bidi.Vis(ln.Text)))
					} else {
						p.println("  " + bidi.Vis(ln.Text))
					}
				}
				return nil
			}
			last, lastID := -2, np.VideoID
			for {
				if np.VideoID != lastID { // next track: look its lyrics up
					lastID, last = np.VideoID, -2
					lines = nil
					if !np.Idle {
						if ly, err := l.Lyrics(cmd.Context(), np.Track, fresh); err == nil {
							lines = ly.Lines()
						}
					}
				}
				if cur := lyrics.Current(lines, np.Pos); cur != last {
					last = cur
					text := ""
					if cur >= 0 {
						text = lines[cur].Text
					}
					if p.json {
						_ = p.JSON(map[string]any{"video_id": np.VideoID, "index": cur, "text": text})
					} else {
						p.println(bidi.Vis(text))
					}
				}
				select {
				case <-cmd.Context().Done():
					return nil
				case <-time.After(250 * time.Millisecond):
				}
				if np, err = l.Now(); err != nil {
					return err
				}
			}
		},
	}
	c.Flags().BoolVarP(&follow, "follow", "f", false, "print each synced line as it's sung")
	c.Flags().BoolVar(&fresh, "fresh", false, "skip the cache and ask lrclib.net again")
	return c
}
