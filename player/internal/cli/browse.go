package cli

import (
	"fmt"
	"strings"

	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/ytm"
	"github.com/spf13/cobra"
)

type artistJSON struct {
	ID       string   `json:"id"`
	Name     string   `json:"name"`
	Tags     []string `json:"tags"`
	Catalogs []string `json:"catalogs"`
	Thumb    string   `json:"thumbnail,omitempty"`
}

func toJSON(as []catalog.Artist) []artistJSON {
	out := make([]artistJSON, len(as))
	for i, a := range as {
		tags := a.TagKeys()
		if tags == nil {
			tags = []string{}
		}
		out[i] = artistJSON{a.ID, a.Name, tags, a.Catalogs, a.Thumbnail}
	}
	return out
}

func tagFlag(c *cobra.Command, dst *string) {
	c.Flags().StringVarP(dst, "tag", "t", "", "only artists with this tag ("+strings.Join(catalog.TagKeys(), ", ")+")")
	c.RegisterFlagCompletionFunc("tag", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return catalog.TagKeys(), cobra.ShellCompDirectiveNoFileComp
	})
}

func (a *app) searchCmd() *cobra.Command {
	var tag, cat string
	var limit int
	c := &cobra.Command{
		Use:     "search <query>",
		Aliases: []string{"s", "find"},
		Short:   "Fuzzy-search the artist catalog",
		Example: "  goose search fried\n  goose search --tag chasidish -n 5 ''\n  goose search avi --json | jq -r '.[0].id'",
		Args:    cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			tg, err := catalog.FindTag(tag)
			if err != nil {
				return usage(err)
			}
			q := strings.Join(args, " ")
			if q == "" && tg == nil && cat == "" {
				return usage(fmt.Errorf("give a query, --tag or --catalog"))
			}
			sub, err := l.Cat.Subset(cat)
			if err != nil {
				return usage(err)
			}
			res := sub.Search(q, tg)
			if limit > 0 && len(res) > limit {
				res = res[:limit]
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(toJSON(res))
			}
			if len(res) == 0 {
				return &exitError{1, fmt.Errorf("no artists match %q", q)}
			}
			for _, ar := range res {
				p.row("", ar.Name, p.S(p.dim, strings.Join(ar.TagLabels(), " · ")))
			}
			return nil
		},
	}
	tagFlag(c, &tag)
	a.catalogFlag(c, &cat)
	c.Flags().IntVarP(&limit, "limit", "n", 20, "max results (0 = all)")
	return c
}

func (a *app) artistsCmd() *cobra.Command {
	var tag, cat string
	c := &cobra.Command{
		Use:     "artists",
		Aliases: []string{"ls"},
		Short:   "List every artist in the catalog (one per line when piped)",
		Example: "  goose artists --tag israeli\n  goose artists | fzf | xargs -I{} goose play {}",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			tg, err := catalog.FindTag(tag)
			if err != nil {
				return usage(err)
			}
			sub, err := l.Cat.Subset(cat)
			if err != nil {
				return usage(err)
			}
			list := sub.Filter(tg)
			p := a.out(cmd)
			if p.json {
				return p.JSON(toJSON(list))
			}
			for _, ar := range list {
				if p.tty {
					p.row("", ar.Name, p.S(p.dim, strings.Join(ar.TagLabels(), " · ")))
				} else {
					p.println(ar.Name)
				}
			}
			return nil
		},
	}
	tagFlag(c, &tag)
	a.catalogFlag(c, &cat)
	return c
}

func (a *app) tagsCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "tags",
		Short: "List catalog tags with artist counts",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			type tagJSON struct {
				Key   string `json:"key"`
				Label string `json:"label"`
				Count int    `json:"count"`
			}
			var out []tagJSON
			for i := range catalog.Tags {
				t := &catalog.Tags[i]
				out = append(out, tagJSON{t.Key, t.Label, len(l.Cat.Filter(t))})
			}
			if p.json {
				return p.JSON(out)
			}
			for _, t := range out {
				p.row("", fmt.Sprintf("%-10s %s", t.Key, p.S(p.dim, t.Label)), p.S(p.accent, fmt.Sprint(t.Count)))
			}
			return nil
		},
	}
}

func (a *app) artistCmd() *cobra.Command {
	var fresh bool
	c := &cobra.Command{
		Use:               "artist <artist>",
		Aliases:           []string{"a", "info"},
		Short:             "Show an artist: albums, singles, top songs, related artists",
		Example:           "  goose artist \"8th Day\"\n  goose artist fried --json | jq '.albums[].title'",
		Args:              cobra.MinimumNArgs(1),
		ValidArgsFunction: a.completeArtist,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			ar, err := l.ResolveArtist(strings.Join(args, " "))
			if err != nil {
				return err
			}
			page, err := l.Artist(a.ctx(cmd), ar.ID, fresh)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(page)
			}
			printArtist(p, page, ar)
			return nil
		},
	}
	c.Flags().BoolVar(&fresh, "fresh", false, "bypass the cache")
	return c
}

func printArtist(p *printer, page *ytm.Artist, ar catalog.Artist) {
	var meta []string
	if page.Subscribers != "" {
		meta = append(meta, page.Subscribers+" subscribers")
	}
	if page.Views != "" {
		meta = append(meta, page.Views)
	}
	meta = append(meta, ar.TagLabels()...)
	p.heading(page.Name, strings.Join(meta, " · "))
	if page.Description != "" {
		desc := strings.Join(strings.Fields(page.Description), " ")
		if len([]rune(desc)) > 240 {
			desc = string([]rune(desc)[:239]) + "…"
		}
		p.println(p.S(p.dim, desc))
	}
	section := func(title string, n int) {
		p.println()
		p.println(p.S(p.accent, title) + p.S(p.dim, fmt.Sprintf(" (%d)", n)))
	}
	n := 0
	if len(page.Albums) > 0 {
		section(albumHeading(page.Albums), len(page.Albums))
		for _, r := range page.Albums {
			n++
			p.row(p.S(p.dim, fmt.Sprintf("%3d  ", n)), r.Title, p.S(p.dim, r.Year))
		}
	}
	if len(page.Singles) > 0 {
		section("Singles & EPs", len(page.Singles))
		for _, r := range page.Singles {
			n++
			p.row(p.S(p.dim, fmt.Sprintf("%3d  ", n)), r.Title, p.S(p.dim, strings.TrimSpace(r.Kind+" "+r.Year)))
		}
	}
	if len(page.TopSongs) > 0 {
		section("Top songs", len(page.TopSongs))
		for _, s := range page.TopSongs {
			p.row("     ", s.Title, p.S(p.dim, strings.TrimSpace(s.Plays+"  "+s.Album)))
		}
	}
	if len(page.Related) > 0 {
		section("Fans also like", len(page.Related))
		for _, r := range page.Related {
			p.row("     ", r.Name, p.S(p.dim, r.Subtitle))
		}
	}
	p.println()
	p.println(p.S(p.dim, fmt.Sprintf("play:  goose play %q <album|number>", page.Name)))
}

func albumHeading(rels []ytm.Release) string {
	if len(rels) == 1 && rels[0].Kind == "Uploads" {
		return "Uploads"
	}
	return "Albums"
}

func (a *app) albumCmd() *cobra.Command {
	var fresh bool
	c := &cobra.Command{
		Use:               "album <artist> <album>",
		Aliases:           []string{"tracks"},
		Short:             "Show an album's tracklist (album = title, fragment, or number from `goose artist`)",
		Example:           "  goose album \"8th Day\" lucky\n  goose album \"8th Day\" 1 --json",
		Args:              cobra.ExactArgs(2),
		ValidArgsFunction: a.completeRelease,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			al, ar, err := a.resolveAlbum(cmd, l, args[0], args[1], fresh)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(al)
			}
			meta := []string{}
			for _, s := range []string{al.Kind, al.Year, ar.Name, fmt.Sprintf("%d tracks", len(al.Tracks)), al.Length} {
				if s != "" {
					meta = append(meta, s)
				}
			}
			p.heading(al.Title, strings.Join(meta, " · "))
			p.println()
			for _, t := range al.Tracks {
				right := ytm.FormatDuration(t.Duration)
				if t.Duration == 0 {
					right = ""
				}
				if t.Plays != "" {
					right = p.S(p.dim, t.Plays) + "  " + right
				}
				p.row(p.S(p.dim, fmt.Sprintf("%3d  ", t.Index)), t.Title, right)
			}
			return nil
		},
	}
	c.Flags().BoolVar(&fresh, "fresh", false, "bypass the cache")
	return c
}

func (a *app) resolveAlbum(cmd *cobra.Command, l *library.Lib, artistQ, albumQ string, fresh bool) (*ytm.Album, *ytm.Artist, error) {
	ar, err := l.ResolveArtist(artistQ)
	if err != nil {
		return nil, nil, err
	}
	page, err := l.Artist(a.ctx(cmd), ar.ID, fresh)
	if err != nil {
		return nil, nil, err
	}
	rel, err := library.ResolveRelease(page, albumQ)
	if err != nil {
		return nil, nil, usage(err)
	}
	al, err := l.Album(a.ctx(cmd), rel.BrowseID, fresh)
	if err != nil {
		return nil, nil, err
	}
	return al, page, nil
}
