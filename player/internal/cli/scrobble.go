package cli

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/library"
	"github.com/lubabs770/gooseneck/player/internal/scrobble"
	"github.com/spf13/cobra"
)

func (a *app) scrobbleCmd() *cobra.Command {
	c := &cobra.Command{
		Use:   "scrobble",
		Short: "Scrobble to ListenBrainz / Last.fm (subcommands: login, logout, flush, on, off)",
		Long: `Scrobble what you listen to: always to a local log (see 'goose scrobbles'),
and optionally to ListenBrainz and/or Last.fm.

  goose scrobble login listenbrainz      paste a token from listenbrainz.org/settings
  goose scrobble login lastfm            approve gooseneck in your browser
  goose scrobble                         status: services, current track, queue

A small background scrobbler starts with playback and exits when the player
does. A track counts after half its length or 4 minutes of actual listening
(pauses and seeks don't count); tracks under 30s never count. Listens made
offline are queued and sent later.`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			p := a.out(cmd)
			st, derr := library.QueryScrobbler()
			if derr != nil {
				st = library.ScrobblerStatus{Queued: l.DB.QueuedScrobbleCount()}
			}
			if p.json {
				return p.JSON(map[string]any{"enabled": config.On(a.cfg.Scrobble, true),
					"listenbrainz": a.cfg.ListenBrainzToken != "", "lastfm": a.cfg.LastfmSession != "",
					"lastfm_user": a.cfg.LastfmUser, "daemon": st})
			}
			mark := func(ok bool) string {
				if ok {
					return p.S(p.ok, "✓")
				}
				return p.S(p.dim, "·")
			}
			lb := "not set up — goose scrobble login listenbrainz"
			if a.cfg.ListenBrainzToken != "" {
				lb = "token " + config.Mask(a.cfg.ListenBrainzToken)
			}
			lf := "not set up — goose scrobble login lastfm"
			if a.cfg.LastfmSession != "" {
				lf = "logged in as " + a.cfg.LastfmUser
			}
			localOn := config.On(a.cfg.LocalScrobble, true)
			local := "off — goose config set local_scrobble true"
			if localOn {
				local = a.cfg.ScrobblesPath()
				if lo, err := l.LocalScrobbles(); err == nil {
					if t, err := lo.Totals(scrobble.Range{}); err == nil {
						local = fmt.Sprintf("%d listens · %s", t.Listens, a.cfg.ScrobblesPath())
					}
				}
			}
			p.row(mark(localOn)+" ", fmt.Sprintf("%-13s", "local")+local, "")
			p.row(mark(a.cfg.ListenBrainzToken != "")+" ", fmt.Sprintf("%-13s", "listenbrainz")+lb, "")
			p.row(mark(a.cfg.LastfmSession != "")+" ", fmt.Sprintf("%-13s", "last.fm")+lf, "")
			if !config.On(a.cfg.Scrobble, true) {
				p.println(p.S(p.warn, "scrobbling is turned off — goose scrobble on"))
			}
			switch {
			case derr == nil && st.Current != nil:
				state := fmt.Sprintf("%s / %s listened", fmtSec(st.Played), fmtSec(st.Needed))
				if st.Scrobbled {
					state = p.S(p.ok, "scrobbled")
				}
				p.println()
				p.println(p.S(p.accent, "♫ ") + st.Current.Track + p.S(p.dim, " · "+st.Current.Artist+"  ") + state)
			case derr == nil:
				p.println(p.S(p.dim, "\nscrobbler running, nothing playing"))
			case len(l.ScrobbleServices()) > 0:
				p.println(p.S(p.dim, "\nscrobbler idle (starts with playback)"))
			}
			for svc, n := range st.Queued {
				p.println(p.S(p.warn, fmt.Sprintf("%d listens queued for %s", n, svc)) + p.S(p.dim, " (goose scrobble flush)"))
			}
			if st.LastError != "" {
				p.println(p.S(p.bad, "last error: ") + st.LastError)
			}
			return nil
		},
	}

	login := &cobra.Command{Use: "login", Short: "Connect a scrobbling service"}
	login.AddCommand(&cobra.Command{
		Use:   "listenbrainz [token]",
		Short: "Use a ListenBrainz user token (listenbrainz.org/settings)",
		Args:  cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			token := ""
			if len(args) == 1 {
				token = args[0]
			} else {
				fmt.Fprint(cmd.ErrOrStderr(), "ListenBrainz user token (https://listenbrainz.org/settings/): ")
				token = readLine()
			}
			token = strings.TrimSpace(token)
			if token == "" {
				return usage(errors.New("no token given"))
			}
			ctx, cancel := context.WithTimeout(cmd.Context(), 20*time.Second)
			defer cancel()
			user, err := (&scrobble.ListenBrainz{Token: token, Endpoint: os.Getenv("GOOSENECK_LISTENBRAINZ_URL")}).Validate(ctx)
			if err != nil {
				return err
			}
			a.cfg.ListenBrainzToken = token
			if err := config.Save(a.cfg); err != nil {
				return err
			}
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + "scrobbling to ListenBrainz as " + p.S(p.title, user))
			return nil
		},
	})
	var key, secret string
	lfm := &cobra.Command{
		Use:   "lastfm",
		Short: "Log in to Last.fm in your browser (needs your own API key)",
		Long: `Log in to Last.fm. Last.fm requires every app to use an API key, so create
one (free, takes a minute) at https://www.last.fm/api/account/create — any
name, callback URL can stay empty — then run this with the key and secret.
They're saved to the config (mode 0600), so later logins don't need them.`,
		Example: "  goose scrobble login lastfm --key 0123… --secret abcd…",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			if key == "" {
				key = a.cfg.LastfmKey
			}
			if secret == "" {
				secret = a.cfg.LastfmSecret
			}
			if key == "" {
				fmt.Fprint(cmd.ErrOrStderr(), "Last.fm API key (https://www.last.fm/api/account/create): ")
				key = strings.TrimSpace(readLine())
			}
			if secret == "" {
				fmt.Fprint(cmd.ErrOrStderr(), "Last.fm shared secret: ")
				secret = strings.TrimSpace(readLine())
			}
			if key == "" || secret == "" {
				return usage(errors.New("Last.fm needs an API key and shared secret"))
			}
			lf := &scrobble.LastFM{Key: key, Secret: secret}
			ctx := cmd.Context()
			token, err := lf.GetToken(ctx)
			if err != nil {
				return err
			}
			url := lf.AuthURL(token)
			fmt.Fprintln(cmd.ErrOrStderr(), "\nApprove gooseneck in your browser:\n  "+url)
			_ = exec.Command("xdg-open", url).Start()
			fmt.Fprint(cmd.ErrOrStderr(), "\nPress enter once you've clicked \"Yes, allow access\"… ")
			readLine()
			session, user, err := lf.GetSession(ctx, token)
			if err != nil {
				return fmt.Errorf("%w (did you approve it in the browser?)", err)
			}
			a.cfg.LastfmKey, a.cfg.LastfmSecret, a.cfg.LastfmSession, a.cfg.LastfmUser = key, secret, session, user
			if err := config.Save(a.cfg); err != nil {
				return err
			}
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + "scrobbling to Last.fm as " + p.S(p.title, user))
			return nil
		},
	}
	lfm.Flags().StringVar(&key, "key", "", "Last.fm API key")
	lfm.Flags().StringVar(&secret, "secret", "", "Last.fm shared secret")
	login.AddCommand(lfm)

	logout := &cobra.Command{
		Use:       "logout <listenbrainz|lastfm>",
		Short:     "Forget a service's credentials",
		Args:      cobra.ExactArgs(1),
		ValidArgs: []string{"listenbrainz", "lastfm"},
		RunE: func(cmd *cobra.Command, args []string) error {
			switch args[0] {
			case "listenbrainz":
				a.cfg.ListenBrainzToken = ""
			case "lastfm", "last.fm":
				a.cfg.LastfmSession, a.cfg.LastfmUser = "", ""
			default:
				return usage(fmt.Errorf("unknown service %q", args[0]))
			}
			if err := config.Save(a.cfg); err != nil {
				return err
			}
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + "logged out of " + args[0])
			return nil
		},
	}
	toggle := func(on bool) *cobra.Command {
		name := map[bool]string{true: "on", false: "off"}[on]
		return &cobra.Command{
			Use: name, Short: "Turn scrobbling " + name, Args: cobra.NoArgs,
			RunE: func(cmd *cobra.Command, args []string) error {
				a.cfg.Scrobble = config.BoolPtr(on)
				if err := config.Save(a.cfg); err != nil {
					return err
				}
				p := a.out(cmd)
				p.println(p.S(p.ok, "✓ ") + "scrobbling " + name)
				return nil
			},
		}
	}
	flush := &cobra.Command{
		Use: "flush", Short: "Send queued listens now", Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			n, err := l.FlushScrobbles(cmd.Context())
			p := a.out(cmd)
			p.println(p.S(p.ok, "✓ ") + fmt.Sprintf("sent %d queued listens", n))
			return err
		},
	}
	daemon := &cobra.Command{
		Use: "daemon", Short: "Run the scrobbler in the foreground (started automatically)", Hidden: true, Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			l, err := a.lib()
			if err != nil {
				return err
			}
			return l.RunDaemon(cmd.Context(), cmd.OutOrStdout())
		},
	}
	c.AddCommand(login, logout, toggle(true), toggle(false), flush, daemon)
	return c
}

func fmtSec(s float64) string {
	d := int(s)
	return fmt.Sprintf("%d:%02d", d/60, d%60)
}

func readLine() string {
	line, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	return strings.TrimSpace(line)
}
