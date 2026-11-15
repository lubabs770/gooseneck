package cli

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"

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
				p.println(p.S(p.dim, "API only — add --ui <dir> to serve a web UI"))
			}
			if open {
				openURL("http://" + addr)
			}
			return web.New(l, opts).Serve(a.ctx(cmd))
		},
	}
	c.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "local listen address")
	c.Flags().StringVar(&ui, "ui", "", "directory of web UI files to serve at /")
	c.Flags().BoolVar(&tailscale, "tailscale", false, "also listen on this machine's tailnet IP")
	c.Flags().BoolVar(&open, "open", false, "open the UI in a browser")
	return c
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
