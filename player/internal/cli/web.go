package cli

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/x/term"
	"github.com/lubabs770/gooseneck/player/internal/config"
	"github.com/lubabs770/gooseneck/player/internal/mpv"
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
	var tailscale, update, noOpen, yes bool
	c := &cobra.Command{
		Use:   "ui",
		Short: "Open the partiality web UI in the background (goose ui stop to stop)",
		Long: `Open partiality, the web UI, in your browser. The server runs in the
background, so your terminal comes straight back; goose ui stop ends it.
For a foreground server use goose serve --ui <dir>.

If partiality isn't installed yet, goose ui asks before cloning
https://github.com/lubabs770/partiality into the config dir (needs git;
--yes skips the question). Set ui in config.toml to use another checkout.
If the server is already running it just opens the browser.

The browser is a remote: music plays on this machine.`,
		Example: "  goose ui                # open it here\n  goose ui --tailscale    # also reachable from your phone\n  goose ui --update       # pull the latest partiality first\n  goose ui stop",
		Args:    cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			dir := a.cfg.UIDir()
			p := a.out(cmd)
			if _, err := os.Stat(dir); errors.Is(err, os.ErrNotExist) {
				if !yes {
					ok, err := confirm(cmd, fmt.Sprintf("partiality (the web UI) isn't installed.\nThis will clone %s\ninto %s\nContinue? [y/N] ", partialityRepo, dir))
					if err != nil || !ok {
						return err
					}
				}
				if err := git(cmd, "clone", "--depth", "1", partialityRepo, dir); err != nil {
					return err
				}
			} else if update {
				if err := git(cmd, "-C", dir, "pull", "--ff-only"); err != nil {
					return err
				}
			}
			if !tailscale && listening(addr) {
				p.println(p.S(p.accent, "● ") + "http://" + addr + p.S(p.dim, " (already running)"))
				if !noOpen {
					openURL("http://" + addr)
				}
				return nil
			}
			if err := startUI(addr, dir, tailscale); err != nil {
				return err
			}
			out, _ := os.ReadFile(uiLog())
			fmt.Fprint(cmd.OutOrStdout(), string(out))
			p.println(p.S(p.dim, "running in the background — goose ui stop to stop"))
			if !noOpen {
				openURL("http://" + addr)
			}
			return nil
		},
	}
	c.Flags().StringVar(&addr, "addr", "127.0.0.1:7777", "local listen address")
	c.Flags().BoolVar(&tailscale, "tailscale", false, "also listen on this machine's tailnet IP")
	c.Flags().BoolVar(&update, "update", false, "git pull the latest partiality before starting")
	c.Flags().BoolVar(&noOpen, "no-open", false, "don't open a browser")
	c.Flags().BoolVarP(&yes, "yes", "y", false, "install partiality without asking")
	c.AddCommand(&cobra.Command{
		Use:   "stop",
		Short: "Stop the background web UI server",
		Args:  cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			p := a.out(cmd)
			if !stopUI() {
				p.println(p.S(p.dim, "the web UI isn't running in the background"))
				return nil
			}
			p.println("stopped the web UI")
			return nil
		},
	})
	return c
}

const partialityRepo = "https://github.com/lubabs770/partiality"

func uiLog() string { return filepath.Join(config.RuntimeDir(), "ui.log") }
func uiPID() string { return filepath.Join(config.RuntimeDir(), "ui.pid") }

func listening(addr string) bool {
	conn, err := net.DialTimeout("tcp", addr, 300*time.Millisecond)
	if err == nil {
		conn.Close()
	}
	return err == nil
}

// confirm asks a yes/no question on the terminal; default no. Without a
// terminal it refuses rather than guessing.
func confirm(cmd *cobra.Command, q string) (bool, error) {
	if !term.IsTerminal(os.Stdin.Fd()) {
		return false, usage(errors.New("partiality isn't installed; run goose ui in a terminal to confirm, or pass --yes"))
	}
	fmt.Fprint(cmd.ErrOrStderr(), q)
	ans, _ := bufio.NewReader(os.Stdin).ReadString('\n')
	ans = strings.ToLower(strings.TrimSpace(ans))
	return ans == "y" || ans == "yes", nil
}

// startUI runs `goose serve` detached, logging to ui.log, and waits for it to
// accept connections.
func startUI(addr, dir string, tailscale bool) error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(config.RuntimeDir(), 0o700); err != nil {
		return err
	}
	logf, err := os.OpenFile(uiLog(), os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logf.Close()
	args := []string{"serve", "--no-color", "--addr", addr, "--ui", dir}
	if tailscale {
		args = append(args, "--tailscale")
	}
	c := exec.Command(exe, args...)
	c.Stdout, c.Stderr = logf, logf
	mpv.Detach(c)
	if err := c.Start(); err != nil {
		return err
	}
	exited := make(chan struct{})
	go func() { _ = c.Wait(); close(exited) }()
	for deadline := time.Now().Add(10 * time.Second); time.Now().Before(deadline); {
		select {
		case <-exited:
			out, _ := os.ReadFile(uiLog())
			return fmt.Errorf("the web UI server exited: %s", strings.TrimSpace(string(out)))
		case <-time.After(100 * time.Millisecond):
		}
		if listening(addr) {
			return os.WriteFile(uiPID(), []byte(strconv.Itoa(c.Process.Pid)), 0o600)
		}
	}
	_ = c.Process.Kill()
	return fmt.Errorf("the web UI server didn't start within 10s (log: %s)", uiLog())
}

// stopUI ends the background server from ui.pid. It reports false when there
// is none.
func stopUI() bool {
	b, err := os.ReadFile(uiPID())
	if err != nil {
		return false
	}
	defer os.Remove(uiPID())
	pid, err := strconv.Atoi(strings.TrimSpace(string(b)))
	if err != nil || !isServe(pid) {
		return false
	}
	proc, err := os.FindProcess(pid)
	if err != nil {
		return false
	}
	if proc.Signal(os.Interrupt) != nil {
		return proc.Kill() == nil
	}
	return true
}

// isServe guards against a stale pid file pointing at a reused pid.
// ponytail: uses ps, so on Windows it trusts the pid file.
func isServe(pid int) bool {
	if runtime.GOOS == "windows" {
		return true
	}
	out, err := exec.Command("ps", "-p", strconv.Itoa(pid), "-o", "command=").Output()
	return err == nil && strings.Contains(string(out), " serve ")
}

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
