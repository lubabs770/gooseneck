package cli

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/spf13/cobra"
)

func (a *app) catalogCmd() *cobra.Command {
	c := &cobra.Command{
		Use:     "catalog",
		Aliases: []string{"catalogs"},
		Short:   "List catalogs; add your own (subcommands: new, add, rm, add-artist, remove-artist, update)",
		Long: `gooseneck merges the main catalog with any extra ones you add: your own
personal list, or someone else's published artists.json.

  goose catalog new mine                         start a personal catalog
  goose catalog add-artist mine "Yaakov Shwekey"  add an artist (YouTube Music search)
  goose catalog add-artist mine <channel id|url> --tag chasidish
  goose catalog add friends https://…/artists.json   follow a published catalog
  goose search --catalog mine ''                 browse just one catalog`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(l.Cat.Sources)
			}
			for _, s := range l.Cat.Sources {
				kind := "remote"
				switch {
				case s.Main:
					kind = "main"
				case s.Editable:
					kind = "personal"
				}
				right := p.S(p.dim, kind+" · updated "+ago(s.Updated))
				mark := p.S(p.ok, "✓ ")
				if s.Err != "" {
					mark, right = p.S(p.bad, "✗ "), p.S(p.bad, s.Err)
				}
				p.row(mark, fmt.Sprintf("%-12s %5d artists", s.Name, s.Count), right)
			}
			p.println(p.S(p.dim, fmt.Sprintf("%d artists in total", l.Cat.Len())))
			return nil
		},
	}

	var urlFlag bool
	add := &cobra.Command{
		Use:   "add <name> <path|url>",
		Short: "Follow an existing catalog file or URL",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name, where := args[0], args[1]
			if err := a.checkNewCatalog(name); err != nil {
				return err
			}
			src := config.CatalogSource{Name: name}
			if urlFlag || strings.HasPrefix(where, "http://") || strings.HasPrefix(where, "https://") {
				src.URL = where
				if err := catalog.Download(where, src.File()); err != nil {
					return err
				}
			} else {
				abs, err := filepath.Abs(where)
				if err != nil {
					return err
				}
				src.Path = abs
			}
			cat, err := catalog.Read(src.File())
			if err != nil {
				return fmt.Errorf("%s isn't a usable catalog: %w", where, err)
			}
			return a.saveCatalog(cmd, src, fmt.Sprintf("added catalog %s (%d artists)", name, cat.Len()))
		},
	}
	add.Flags().BoolVar(&urlFlag, "url", false, "treat the location as a URL")

	newCmd := &cobra.Command{
		Use:   "new <name> [path]",
		Short: "Create an empty personal catalog",
		Args:  cobra.RangeArgs(1, 2),
		RunE: func(cmd *cobra.Command, args []string) error {
			name := args[0]
			if err := a.checkNewCatalog(name); err != nil {
				return err
			}
			path := filepath.Join(config.Dir(), "catalogs", name+".json")
			if len(args) == 2 {
				var err error
				if path, err = filepath.Abs(args[1]); err != nil {
					return err
				}
			}
			if _, err := os.Stat(path); err == nil {
				return fmt.Errorf("%s already exists (use `goose catalog add %s %s` to follow it)", path, name, path)
			}
			if err := catalog.Write(path, nil); err != nil {
				return err
			}
			return a.saveCatalog(cmd, config.CatalogSource{Name: name, Path: path},
				fmt.Sprintf("created catalog %s → %s\n  add artists: goose catalog add-artist %s \"<artist name>\"", name, path, name))
		},
	}

	var del bool
	rm := &cobra.Command{
		Use:               "rm <name>",
		Short:             "Stop using a catalog (--delete also removes a personal catalog's file)",
		Args:              cobra.ExactArgs(1),
		ValidArgsFunction: a.completeCatalog,
		RunE: func(cmd *cobra.Command, args []string) error {
			i := a.catalogIndex(args[0])
			if i < 0 {
				return usage(fmt.Errorf("no extra catalog named %q", args[0]))
			}
			src := a.cfg.Catalogs[i]
			a.cfg.Catalogs = append(a.cfg.Catalogs[:i], a.cfg.Catalogs[i+1:]...)
			if err := config.Save(a.cfg); err != nil {
				return err
			}
			msg := "removed catalog " + src.Name
			if del || src.URL != "" { // URL catalogs only have our cached copy
				if err := os.Remove(src.File()); err == nil {
					msg += " and deleted " + src.File()
				}
			}
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + msg)
			return nil
		},
	}
	rm.Flags().BoolVar(&del, "delete", false, "also delete the catalog file")

	var tags []string
	addArtist := &cobra.Command{
		Use:   "add-artist <catalog> <artist name | channel id | YouTube Music URL>",
		Short: "Add an artist to a personal catalog",
		Example: "  goose catalog add-artist mine \"Yaakov Shwekey\"\n" +
			"  goose catalog add-artist mine https://music.youtube.com/channel/UC… --tag chasidish --tag famous",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: a.completeCatalog,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			src, err := a.editable(args[0])
			if err != nil {
				return err
			}
			q := strings.Join(args[1:], " ")
			id := catalog.ChannelID(q)
			if id == "" {
				res, err := l.YTM.SearchArtists(cmd.Context(), q)
				if err != nil {
					return err
				}
				if len(res) == 0 {
					return fmt.Errorf("no YouTube Music artist matches %q", q)
				}
				id = res[0].ID
			}
			page, err := l.Artist(cmd.Context(), id, true)
			if err != nil {
				return fmt.Errorf("%s: %w", id, err)
			}
			ar := catalog.Artist{ID: id, Name: page.Name, Thumbnail: page.Thumb}
			if existing, ok := l.Cat.Get(id); ok {
				ar = existing // keep curated name, picture and tags
			}
			for _, t := range tags {
				tg, err := catalog.FindTag(t)
				if err != nil || tg == nil {
					return usage(fmt.Errorf("unknown tag %q (valid: %s)", t, strings.Join(catalog.TagKeys(), ", ")))
				}
				setTag(&ar, tg.Key)
			}
			isNew, err := catalog.AddTo(src.File(), ar)
			if err != nil {
				return err
			}
			p := a.out(cmd)
			if p.json {
				return p.JSON(map[string]any{"catalog": src.Name, "added": isNew, "artist": ar})
			}
			verb := "added"
			if !isNew {
				verb = "updated"
			}
			extra := ""
			if rels := len(page.Albums) + len(page.Singles); rels > 0 {
				extra = fmt.Sprintf(" (%d releases)", rels)
			}
			p.println(p.S(p.ok, "✓ ") + verb + " " + p.S(p.title, ar.Name) + p.S(p.dim, extra+" in "+src.Name))
			return nil
		},
	}
	addArtist.Flags().StringArrayVarP(&tags, "tag", "t", nil, "tag the artist ("+strings.Join(catalog.TagKeys(), ", ")+")")

	removeArtist := &cobra.Command{
		Use:               "remove-artist <catalog> <artist>",
		Short:             "Remove an artist from a personal catalog",
		Args:              cobra.MinimumNArgs(2),
		ValidArgsFunction: a.completeCatalog,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			src, err := a.editable(args[0])
			if err != nil {
				return err
			}
			sub, err := l.Cat.Subset(src.Name)
			if err != nil {
				return err
			}
			ar, err := sub.Resolve(strings.Join(args[1:], " "))
			if err != nil {
				return err
			}
			if _, err := catalog.RemoveFrom(src.File(), ar.ID); err != nil {
				return err
			}
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + "removed " + ar.Name + " from " + src.Name)
			return nil
		},
	}

	update := &cobra.Command{
		Use:   "update",
		Short: "Download the latest main catalog and every URL catalog",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.out(cmd)
			dest := config.CatalogCachePath()
			if err := catalog.Download(a.cfg.ArtistsURL, dest); err != nil {
				return err
			}
			main, err := catalog.Read(dest)
			if err != nil {
				return err
			}
			res := map[string]int{catalog.MainName: main.Len()}
			if !p.json {
				p.println(p.S(p.ok, "✓ ") + fmt.Sprintf("%s: %d artists", catalog.MainName, main.Len()))
			}
			var failed error
			for _, src := range a.cfg.Catalogs {
				if src.URL == "" || src.Disabled {
					continue
				}
				if err := catalog.Download(src.URL, src.File()); err != nil {
					failed = err
					if !p.json {
						p.println(p.S(p.bad, "✗ ") + src.Name + ": " + err.Error())
					}
					continue
				}
				c, err := catalog.Read(src.File())
				if err != nil {
					failed = err
					continue
				}
				res[src.Name] = c.Len()
				if !p.json {
					p.println(p.S(p.ok, "✓ ") + fmt.Sprintf("%s: %d artists", src.Name, c.Len()))
				}
			}
			if p.json {
				return p.JSON(res)
			}
			if a.cfg.ArtistsJSON != "" {
				p.println(p.S(p.warn, "note: ") + "artists_json is set in your config, so that file is used as the main catalog")
			}
			return failed
		},
	}
	c.AddCommand(newCmd, add, rm, addArtist, removeArtist, update)
	return c
}

func setTag(a *catalog.Artist, key string) {
	switch key {
	case "famous":
		a.IsFamous = true
	case "chasidish":
		a.IsChasid = true
	case "israeli":
		a.IsIsraeli = true
	case "american":
		a.IsAmerican = true
	case "female":
		a.IsFemale = true
	case "kids":
		a.IsKidZone = true
	case "dj":
		a.IsDJ = true
	case "acapella":
		a.IsAcapellaOnly = true
	}
}

func (a *app) catalogIndex(name string) int {
	for i, c := range a.cfg.Catalogs {
		if c.Name == name {
			return i
		}
	}
	return -1
}

func (a *app) checkNewCatalog(name string) error {
	if a.cfgErr != nil {
		return a.cfgErr
	}
	if err := catalog.ValidName(name); err != nil {
		return usage(err)
	}
	if a.catalogIndex(name) >= 0 {
		return usage(fmt.Errorf("a catalog named %q already exists", name))
	}
	return nil
}

func (a *app) editable(name string) (config.CatalogSource, error) {
	i := a.catalogIndex(name)
	if i < 0 {
		if name == catalog.MainName {
			return config.CatalogSource{}, usage(errors.New("the main catalog is read-only; make your own: goose catalog new mine"))
		}
		return config.CatalogSource{}, usage(fmt.Errorf("no catalog named %q — create it: goose catalog new %s", name, name))
	}
	src := a.cfg.Catalogs[i]
	if src.URL != "" {
		return src, usage(fmt.Errorf("%s follows a URL and is read-only", name))
	}
	return src, nil
}

func (a *app) saveCatalog(cmd *cobra.Command, src config.CatalogSource, msg string) error {
	a.cfg.Catalogs = append(a.cfg.Catalogs, src)
	if err := config.Save(a.cfg); err != nil {
		return err
	}
	p := a.out(cmd)
	if p.json {
		return p.JSON(src)
	}
	p.println(p.S(p.ok, "✓ ") + msg)
	return nil
}

func (a *app) completeCatalog(cmd *cobra.Command, args []string, _ string) ([]string, cobra.ShellCompDirective) {
	if len(args) > 0 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	for _, c := range a.cfg.Catalogs {
		out = append(out, c.Name)
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// catalogFlag adds --catalog to a listing command.
func (a *app) catalogFlag(c *cobra.Command, dst *string) {
	c.Flags().StringVarP(dst, "catalog", "c", "", "only artists from this catalog")
	c.RegisterFlagCompletionFunc("catalog", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		names := []string{catalog.MainName}
		for _, s := range a.cfg.Catalogs {
			names = append(names, s.Name)
		}
		return names, cobra.ShellCompDirectiveNoFileComp
	})
}
