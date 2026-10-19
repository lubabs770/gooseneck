package cli

import (
	"encoding/csv"
	"fmt"
	"strconv"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/bidi"
	"github.com/lubabs770/gooseneck/player/internal/scrobble"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
	"github.com/spf13/cobra"
)

func sinceFlag(c *cobra.Command, dst *string, def string) {
	c.Flags().StringVarP(dst, "since", "s", def, "period: today, week, month, year, all, or 7d / 12h")
	c.RegisterFlagCompletionFunc("since", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"today", "week", "month", "year", "all"}, cobra.ShellCompDirectiveNoFileComp
	})
}

func humanTime(sec int) string {
	h := sec / 3600
	if h >= 1 {
		return fmt.Sprintf("%dh %dm", h, (sec%3600)/60)
	}
	return fmt.Sprintf("%dm", sec/60)
}

func (a *app) scrobblesCmd() *cobra.Command {
	var since string
	var n int
	c := &cobra.Command{
		Use:     "scrobbles",
		Aliases: []string{"listens"},
		Short:   "Your local listening log (subcommands: top, stats, export)",
		Long: `Every listen that counts (half the track or 4 minutes) is recorded in a
local SQLite file, scrobbles.db, whether or not you use ListenBrainz or
Last.fm. Turn it off with: goose config set local_scrobble false`,
		Example: "  goose scrobbles -s today\n  goose scrobbles top artist -s month\n  goose scrobbles stats -s year\n  goose scrobbles export --format csv > listens.csv",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lo, r, err := a.localRange(since)
			if err != nil {
				return err
			}
			list, err := lo.Recent(r, n)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(list)
			}
			if len(list) == 0 {
				p.println(p.S(p.dim, "no listens recorded yet — they appear once a track has played halfway"))
				return nil
			}
			for _, l := range list {
				p.row("", l.Track+p.S(p.dim, " · "+bidi.Vis(l.Artist)), p.S(p.dim, ago(l.StartedAt)))
			}
			return nil
		},
	}
	sinceFlag(c, &since, "all")
	c.Flags().IntVarP(&n, "limit", "n", 25, "how many (0 = all)")

	var topSince string
	var topN int
	top := &cobra.Command{
		Use:       "top [artist|track|album]",
		Short:     "Most-listened artists, tracks or albums",
		Args:      cobra.MaximumNArgs(1),
		ValidArgs: []string{"artist", "track", "album"},
		RunE: func(cmd *cobra.Command, args []string) error {
			what := "artist"
			if len(args) == 1 {
				what = args[0]
			}
			lo, r, err := a.localRange(topSince)
			if err != nil {
				return err
			}
			list, err := lo.Top(what, r, topN)
			if err != nil {
				return usage(err)
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(list)
			}
			for i, c := range list {
				name := bidi.Vis(c.Name)
				if c.Artist != "" {
					name += p.S(p.dim, " · "+bidi.Vis(c.Artist))
				}
				p.row(p.S(p.dim, fmt.Sprintf("%3d  ", i+1)), name,
					p.S(p.accent, strconv.Itoa(c.Listens))+p.S(p.dim, plural(c.Listens, " listen")+" · "+humanTime(c.Seconds)))
			}
			return nil
		},
	}
	sinceFlag(top, &topSince, "month")
	top.Flags().IntVarP(&topN, "limit", "n", 10, "how many")

	var statSince string
	stats := &cobra.Command{
		Use:   "stats",
		Short: "Listening totals for a period",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lo, r, err := a.localRange(statSince)
			if err != nil {
				return err
			}
			t, err := lo.Totals(r)
			if err != nil {
				return err
			}
			arts, _ := lo.Top("artist", r, 3)
			p := a.out(cmd)
			if p.json {
				return p.JSON(map[string]any{"totals": t, "top_artists": arts})
			}
			p.heading("Listening, "+periodLabel(statSince), "")
			p.row("  ", "listens", p.S(p.accent, strconv.Itoa(t.Listens)))
			p.row("  ", "time", humanTime(t.Seconds))
			p.row("  ", "artists", strconv.Itoa(t.Artists))
			p.row("  ", "different tracks", strconv.Itoa(t.Tracks))
			p.row("  ", "days with music", strconv.Itoa(t.Days))
			for i, c := range arts {
				label := map[int]string{0: "top artist", 1: "", 2: ""}[i]
				p.row("  ", fmt.Sprintf("%-16s", label)+bidi.Vis(c.Name), p.S(p.dim, fmt.Sprintf("%d%s", c.Listens, plural(c.Listens, " listen"))))
			}
			return nil
		},
	}
	sinceFlag(stats, &statSince, "month")

	var expSince, format string
	export := &cobra.Command{
		Use:   "export",
		Short: "Export listens (listenbrainz JSON for import, csv, or json)",
		Example: "  goose scrobbles export --format listenbrainz > listens.json   # ListenBrainz → Import\n" +
			"  goose scrobbles export --format csv -s year > 2026.csv",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			lo, r, err := a.localRange(expSince)
			if err != nil {
				return err
			}
			list, err := lo.Recent(r, 0)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			switch format {
			case "json":
				return p.JSON(list)
			case "listenbrainz":
				out := make([]map[string]any, len(list))
				for i, l := range list {
					meta := map[string]any{"artist_name": l.Artist, "track_name": l.Track,
						"additional_info": map[string]any{"duration_ms": l.Duration * 1000,
							"origin_url": "https://music.youtube.com/watch?v=" + l.VideoID, "submission_client": "gooseneck"}}
					if l.Album != "" {
						meta["release_name"] = l.Album
					}
					out[i] = map[string]any{"listened_at": l.StartedAt.Unix(), "track_metadata": meta}
				}
				return p.JSON(out)
			case "csv":
				w := csv.NewWriter(cmd.OutOrStdout())
				_ = w.Write([]string{"listened_at", "artist", "track", "album", "duration", "video_id"})
				for _, l := range list {
					_ = w.Write([]string{l.StartedAt.Format(time.RFC3339), l.Artist, l.Track, l.Album,
						ytm.FormatDuration(l.Duration), l.VideoID})
				}
				w.Flush()
				return w.Error()
			}
			return usage(fmt.Errorf("format must be listenbrainz, csv or json (got %q)", format))
		},
	}
	sinceFlag(export, &expSince, "all")
	export.Flags().StringVarP(&format, "format", "f", "listenbrainz", "listenbrainz | csv | json")
	export.RegisterFlagCompletionFunc("format", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return []string{"listenbrainz", "csv", "json"}, cobra.ShellCompDirectiveNoFileComp
	})
	c.AddCommand(top, stats, export)
	return c
}

func (a *app) localRange(since string) (*scrobble.Local, scrobble.Range, error) {
	l, err := a.lib()
	if err != nil {
		return nil, scrobble.Range{}, err
	}
	r, err := scrobble.ParseSince(since, time.Now())
	if err != nil {
		return nil, r, usage(err)
	}
	lo, err := l.LocalScrobbles()
	return lo, r, err
}

func plural(n int, word string) string {
	if n == 1 {
		return word
	}
	return word + "s"
}

func periodLabel(s string) string {
	switch s {
	case "", "all", "ever":
		return "all time"
	case "today":
		return "today"
	case "week":
		return "the last 7 days"
	case "month":
		return "the last month"
	case "year":
		return "the last year"
	}
	return "the last " + s
}
