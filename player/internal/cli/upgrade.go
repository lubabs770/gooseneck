package cli

import (
	"errors"
	"fmt"
	"runtime"

	"github.com/lubabs770/gooseneck/player/internal/selfupdate"
	"github.com/spf13/cobra"
)

func (a *app) upgradeCmd() *cobra.Command {
	var check, force bool
	var version string
	c := &cobra.Command{
		Use:   "upgrade",
		Short: "Upgrade goose to the latest release",
		Long: `Download the latest gooseneck release for this platform and replace the
running binary with it. The download is checked against the release's
checksums and test-run before anything is replaced.

A running player or scrobble daemon keeps the old version until it restarts.`,
		Example: `  goose upgrade                    # install the newest release
  goose upgrade --check            # only say whether one is available
  goose upgrade --version v0.4.0   # install a specific release (can downgrade)`,
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.out(cmd)
			u := selfupdate.New()
			ctx := a.ctx(cmd)

			var rel selfupdate.Release
			var err error
			if version != "" {
				rel, err = u.ByTag(ctx, version)
			} else {
				rel, err = u.Latest(ctx)
			}
			if err != nil {
				return err
			}

			newer := selfupdate.Newer(a.version, rel.Tag)
			if check {
				if p.json {
					return p.JSON(map[string]any{
						"current": a.version, "latest": rel.Tag, "update_available": newer, "url": rel.URL,
					})
				}
				if newer {
					p.printf("%s is available (you have %s). Run `goose upgrade`.\n", rel.Tag, a.version)
				} else {
					p.printf("gooseneck %s is up to date.\n", a.version)
				}
				return nil
			}

			switch {
			case force:
			case version == "" && selfupdate.IsDev(a.version):
				return usage(fmt.Errorf("this is a development build (%s); pass --force to replace it with %s", a.version, rel.Tag))
			case rel.Tag == a.version:
				p.printf("gooseneck %s is already installed.\n", a.version)
				return nil
			case version == "" && !newer:
				p.printf("gooseneck %s is up to date.\n", a.version)
				return nil
			}

			exe, err := selfupdate.Executable()
			if err != nil {
				return fmt.Errorf("can't find the running binary: %w", err)
			}
			cmd.PrintErrf("Downloading %s for %s/%s…\n", rel.Tag, runtime.GOOS, runtime.GOARCH)
			if err := u.Install(ctx, rel, exe); err != nil {
				if ctx.Err() != nil {
					return errors.New("upgrade cancelled; nothing was changed")
				}
				return err
			}
			if p.json {
				return p.JSON(map[string]string{"from": a.version, "to": rel.Tag, "path": exe})
			}
			p.printf("Upgraded gooseneck %s → %s (%s)\n", a.version, rel.Tag, exe)
			return nil
		},
	}
	c.Flags().BoolVar(&check, "check", false, "only check whether a newer release exists")
	c.Flags().BoolVar(&force, "force", false, "reinstall even if up to date or on a development build")
	c.Flags().StringVar(&version, "version", "", "install this release tag instead of the latest")
	return c
}
