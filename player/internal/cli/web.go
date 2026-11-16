package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/lubabs770/gooseneck/player/internal/web"
	"github.com/spf13/cobra"
)

func (a *app) serveCmd() *cobra.Command {
	var addr, ui string
	var tailscale, open bool
	c := &cobra.Command{
		Use:   "serve",
		Short: "Serve the JSON/SSE API (and a web UI with --ui) for remote control",
		Long: `Serve gooseneck over HTTP: a JSON API for everything the CLI can do, plus
live player state over server-sent events at /api/events. The browser is a
remote: music still plays on this machine, through the same player the TUI
and CLI control.

It listens on localhost only. --tailscale also listens on this machine's
tailnet address, so phones and other computers on your tailnet can use it;
there is no login, the tailnet is the access control.

--ui serves a directory of static files at / (for example partiality,
https://github.com/lubabs770/partiality).`,
		Example: "  goose serve --ui ~/code/partiality --open\n  goose serve --ui ~/code/partiality --tailscale",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.serve(cmd, addr, ui, tailscale, open)
		},
	}
	c.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "local listen address")
	c.Flags().StringVar(&ui, "ui", "", "directory of web UI files to serve at /")
	c.Flags().BoolVar(&tailscale, "tailscale", false, "also listen on this machine's tailnet IP")
	c.Flags().BoolVar(&open, "open", false, "open the UI in a browser")
	return c
}

func (a *app) uiCmd() *cobra.Command {
	var addr string
	var tailscale, update, noOpen bool
	c := &cobra.Command{
		Use:   "ui",
		Short: "Open the partiality web UI (installs it the first time)",
		Long: `Open partiality, the web UI, in your browser: goose serve with the UI
directory filled in. The first run clones https://github.com/lubabs770/partiality
into the config dir (needs git); set ui in config.toml to use another
checkout. If the server is already running it just opens the browser.

Like serve, the browser is a remote: music plays on this machine.`,
		Example: "  goose ui                # open it here\n  goose ui --tailscale    # also reachable from your phone\n  goose ui --update       # pull the latest partiality first",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := a.cfg.UIDir()
			p := a.out(cmd)
			if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
				p.println(p.S(p.dim, "installing partiality → "+dir))
				if err := git(cmd, "clone", "--depth", "1", partialityRepo, dir); err != nil {
					return err
				}
			} else if update {
				if err := git(cmd, "-C", dir, "pull", "--ff-only"); err != nil {
					return err
				}
			}
			if !tailscale {
				if conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond); err == nil {
					conn.Close()
					p.println(p.S(p.accent, "● ") + "http://" + addr + p.S(p.dim, " (already running)"))
					if !noOpen {
						openURL("http://" + addr)
					}
					return nil
				}
			}
			return a.serve(cmd, addr, dir, tailscale, !noOpen)
		},
	}
	c.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "local listen address")
	c.Flags().BoolVar(&tailscale, "tailscale", false, "also listen on this machine's tailnet IP")
	c.Flags().BoolVar(&update, "update", false, "git pull the latest partiality before starting")
	c.Flags().BoolVar(&noOpen, "no-open", false, "don't open a browser")
	return c
}

const partialityRepo = "https://github.com/lubabs770/partiality"

func git(cmd *cobra.Command, args ...string) error {
	g := exec.CommandContext(cmd.Context(), "git", args...)
	g.Stdout, g.Stderr = cmd.ErrOrStderr(), cmd.ErrOrStderr()
	if err := g.Run(); err != nil {
		return fmt.Errorf("git %s: %w", args[0], err)
	}
	return nil
}

func (a *app) serve(cmd *cobra.Command, addr, ui string, tailscale, open bool) error {
	l, err := a.lib()
	if err != nil {
		return err
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return usage(fmt.Errorf("--addr: %w", err))
	}
	if ip := net.ParseIP(host); ip == nil || ip.IsUnspecified() {
		return usage(errors.New("--addr must be a specific IP (127.0.0.1); use --tailscale for the tailnet, never 0.0.0.0"))
	}
	opts := web.Options{Addrs: []string{addr}}
	if ui != "" {
		if opts.UI, err = filepath.Abs(ui); err != nil {
			return err
		}
	}
	if tailscale {
		ip, dns, err := tailnetSelf()
		if err != nil {
			return err
		}
		opts.Addrs = append(opts.Addrs, net.JoinHostPort(ip, port))
		if dns != "" {
			opts.Hosts = append(opts.Hosts, dns)
		}
	}
	p := a.out(cmd)
	for _, ad := range opts.Addrs {
		p.println(p.S(p.accent, "● ") + "http://" + ad)
	}
	for _, h := range opts.Hosts {
		p.println(p.S(p.accent, "● ") + "http://" + net.JoinHostPort(h, port))
	}
	if ui == "" {
		p.println(p.S(p.dim, "API only — add --ui <dir> to serve a web UI, or run goose ui"))
	}
	if open {
		openURL("http://" + addr)
	}
	return web.New(l, opts).Serve(a.ctx(cmd))
}

// tailnetSelf returns this machine's tailnet IPv4 and MagicDNS name.
func tailnetSelf() (ip, dns string, err error) {
	out, err := exec.Command("tailscale", "status", "--json").Output()
	if err != nil {
		return "", "", fmt.Errorf("--tailscale: can't run `tailscale status` (is Tailscale installed and up?): %w", err)
	}
	var st struct {
		Self struct {
			DNSName      string
			TailscaleIPs []string
		}
	}
	if err := json.Unmarshal(out, &st); err != nil {
		return "", "", fmt.Errorf("--tailscale: %w", err)
	}
	for _, s := range st.Self.TailscaleIPs {
		if p := net.ParseIP(s); p != nil && p.To4() != nil {
			return s, strings.TrimSuffix(st.Self.DNSName, "."), nil
		}
	}
	return "", "", errors.New("--tailscale: this machine has no tailnet IPv4 address (is Tailscale connected?)")
}

func openURL(u string) {
	name := "xdg-open"
	switch runtime.GOOS {
	case "darwin":
		name = "open"
	case "windows":
		_ = exec.Command("rundll32", "url.dll,FileProtocolHandler", u).Start()
		return
	}
	_ = exec.Command(name, u).Start()
}
