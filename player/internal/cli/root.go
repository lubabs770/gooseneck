// Package cli is gooseneck's command line. Every TUI feature has a command
// here, so the whole app is scriptable without ever drawing the TUI.
package cli

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"strings"

	"github.com/charmbracelet/x/term"
	"github.com/lubabs770/gooseneck/player/internal/catalog"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/tui"
	"github.com/spf13/cobra"
)

// exitError carries a specific process exit code.
type exitError struct {
	code int
	err  error
}

func (e *exitError) Error() string { return e.err.Error() }
func (e *exitError) Unwrap() error { return e.err }

// usage marks an error as a usage problem (exit code 2).
func usage(err error) error { return &exitError{2, err} }

type app struct {
	version string
	asJSON  bool
	noColor bool

	cfg    config.Config
	cfgErr error
	l      *library.Lib
}

// lib opens the library on first use, so commands that don't need it (version,
// completion, config path) stay instant and work without a catalog.
func (a *app) lib() (*library.Lib, error) {
	if a.l != nil {
		return a.l, nil
	}
	if a.cfgErr != nil {
		return nil, fmt.Errorf("config: %w", a.cfgErr)
	}
	l, err := library.Open(a.cfg)
	if err != nil {
		return nil, err
	}
	a.l = l
	return l, nil
}

func (a *app) out(cmd *cobra.Command) *printer {
	return newPrinter(cmd.OutOrStdout(), a.asJSON, a.noColor, tui.GetTheme(a.cfg.Theme))
}

// ctx is cancelled by ctrl-c.
func (a *app) ctx(cmd *cobra.Command) context.Context { return cmd.Context() }

// Execute runs the CLI and returns the process exit code.
func Execute(version string) int {
	a := &app{version: version}
	a.cfg, a.cfgErr = config.Load()
	root := a.rootCmd()
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()
	err := root.ExecuteContext(ctx)
	if a.l != nil {
		a.l.Close()
	}
	if err == nil {
		return 0
	}
	fmt.Fprintln(os.Stderr, "goose:", err)
	var ee *exitError
	var amb *catalog.AmbiguousError
	switch {
	case errors.As(err, &ee):
		return ee.code
	case errors.As(err, &amb):
		return 2
	}
	return 1
}

func (a *app) rootCmd() *cobra.Command {
	root := &cobra.Command{
		Use:   "goose",
		Short: "Jewish music in your terminal — browse ~1600 artists and stream them through mpv",
		Long: `gooseneck: a TUI and CLI for the gooseneck catalog of Jewish music artists.

Run with no arguments in a terminal to open the TUI. Every TUI action is also
a command, so gooseneck is fully scriptable:

  goose search avraham              find artists
  goose artist "8th Day"            discography, top songs, related artists
  goose play "8th Day" lucky        play an album (fuzzy names work)
  goose play "8th Day" --shuffle    shuffle everything they've released
  goose next / pause / now          control playback from any shell
  goose artists | fzf | xargs -I{} goose play {}

Add --json to any listing for machine-readable output.`,
		Version:       a.version,
		SilenceUsage:  true,
		SilenceErrors: true,
		Args:          cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if !term.IsTerminal(os.Stdin.Fd()) || !term.IsTerminal(os.Stdout.Fd()) {
				return cmd.Help()
			}
			return a.runTUI(cmd, tui.Options{})
		},
	}
	root.PersistentFlags().BoolVar(&a.asJSON, "json", false, "machine-readable JSON output")
	root.PersistentFlags().BoolVar(&a.noColor, "no-color", false, "disable colors (also: NO_COLOR=1)")
	root.SetVersionTemplate("gooseneck {{.Version}}\n")

	root.AddGroup(
		&cobra.Group{ID: "browse", Title: "Browse:"},
		&cobra.Group{ID: "play", Title: "Playback:"},
		&cobra.Group{ID: "more", Title: "More:"},
	)
	for _, c := range []*cobra.Command{a.searchCmd(), a.artistsCmd(), a.tagsCmd(), a.artistCmd(), a.albumCmd(), a.downloadCmd(), a.downloadsCmd()} {
		c.GroupID = "browse"
		root.AddCommand(c)
	}
	for _, c := range a.playbackCmds() {
		c.GroupID = "play"
		root.AddCommand(c)
	}
	for _, c := range []*cobra.Command{a.tuiCmd(), a.scrobbleCmd(), a.scrobblesCmd(), a.historyCmd(), a.topCmd(), a.catalogCmd(),
		a.cacheCmd(), a.configCmd(), a.doctorCmd(), a.versionCmd()} {
		c.GroupID = "more"
		root.AddCommand(c)
	}
	root.AddCommand(&cobra.Command{
		Use: "daemon", Short: "Background helper for scrobbling and radio (started automatically)",
		Hidden: true, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			return l.RunDaemon(cmd.Context(), cmd.OutOrStdout())
		},
	})
	root.SetHelpCommandGroupID("more")
	root.SetCompletionCommandGroupID("more")
	return root
}

func (a *app) tuiCmd() *cobra.Command {
	var opts tui.Options
	c := &cobra.Command{
		Use:   "tui",
		Short: "Open the full-screen interface (the default with no arguments)",
		Args:  cobra.NoArgs,
		RunE:  func(cmd *cobra.Command, args []string) error { return a.runTUI(cmd, opts) },
	}
	c.Flags().StringVar(&opts.Theme, "theme", "", "theme to start with (see `goose config set theme`)")
	c.Flags().StringVar(&opts.Artist, "artist", "", "open straight to this artist")
	c.RegisterFlagCompletionFunc("theme", func(*cobra.Command, []string, string) ([]string, cobra.ShellCompDirective) {
		return tui.ThemeNames(), cobra.ShellCompDirectiveNoFileComp
	})
	c.RegisterFlagCompletionFunc("artist", a.completeArtist)
	return c
}

func (a *app) runTUI(cmd *cobra.Command, opts tui.Options) error {
	l, err := a.lib()
	if err != nil {
		return err
	}
	opts.Version = a.version
	return tui.Run(l, opts)
}

func (a *app) versionCmd() *cobra.Command {
	return &cobra.Command{
		Use:   "version",
		Short: "Print the version",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if a.asJSON {
				return a.out(cmd).JSON(map[string]string{"version": a.version})
			}
			fmt.Fprintln(cmd.OutOrStdout(), "gooseneck", a.version)
			return nil
		},
	}
}

// completeArtist completes catalog names for the first positional argument.
func (a *app) completeArtist(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	cat, err := catalog.LoadAll(a.cfg)
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	q := strings.ToLower(toComplete)
	var out []string
	for _, ar := range cat.Artists {
		if q == "" || strings.HasPrefix(strings.ToLower(ar.Name), q) {
			out = append(out, ar.Name)
		}
	}
	if len(out) == 0 && q != "" {
		for i, ar := range cat.Search(toComplete, nil) {
			if i == 50 {
				break
			}
			out = append(out, ar.Name)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}

// completeRelease completes album titles for the second positional argument,
// using only the cache (completion must never wait on the network).
func (a *app) completeRelease(cmd *cobra.Command, args []string, toComplete string) ([]string, cobra.ShellCompDirective) {
	if len(args) == 0 {
		return a.completeArtist(cmd, args, toComplete)
	}
	if len(args) > 1 {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	l, err := a.lib()
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	ar, err := l.ResolveArtist(args[0])
	if err != nil {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	page, ok := l.CachedArtist(ar.ID)
	if !ok {
		return nil, cobra.ShellCompDirectiveNoFileComp
	}
	var out []string
	q := strings.ToLower(toComplete)
	for _, r := range page.Releases() {
		if strings.HasPrefix(strings.ToLower(r.Title), q) {
			out = append(out, r.Title)
		}
	}
	return out, cobra.ShellCompDirectiveNoFileComp
}
