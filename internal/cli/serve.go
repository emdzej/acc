package cli

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/exec"
	"os/signal"
	"runtime"
	"strings"
	"syscall"
	"time"

	"github.com/spf13/cobra"

	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/hook"
	"github.com/emdzej/acc/internal/model"
	"github.com/emdzej/acc/internal/server"
	"github.com/emdzej/acc/internal/store"
)

func serveCmd() *cobra.Command {
	var addr string
	var o serveOpts
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the ACC daemon (API, live stream and web dashboard)",
		Long: `Runs the ACC daemon. By default it listens on 127.0.0.1 only and every
client needs the local token (~/.acc/token).

To use the dashboard from other devices, listen on all interfaces:

  acc serve --addr 0.0.0.0:7317                    # sign in with ~/.acc/remote_key
  acc serve --addr 0.0.0.0:7317 --tls-cert c.pem --tls-key k.pem
  acc serve --addr 0.0.0.0:7317 --auth none         # no sign-in at all

ACC explains what that exposes and asks you to confirm (--yes skips the
question, e.g. under a service manager). Whoever gets in can start agent
sessions and prompt them: treat access as running code on this machine.
Requests by unknown host names are refused (DNS rebinding); add names
with --allow-host.`,
		RunE: func(cmd *cobra.Command, _ []string) error {
			if o.auth != authKey && o.auth != authNone {
				return fmt.Errorf("--auth must be key or none")
			}
			if (o.tlsCert == "") != (o.tlsKey == "") {
				return fmt.Errorf("--tls-cert and --tls-key go together")
			}
			if o.exposed(addr) {
				if err := confirmExposure(addr, &o); err != nil {
					return err
				}
			}
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			d, err := startDaemon(addr, log, o)
			if err != nil {
				return err
			}
			defer d.Close()
			fmt.Fprintf(os.Stderr, "Dashboard: run `acc open`\n")
			if !isLoopback(addr) {
				fmt.Fprintf(os.Stderr, "Other devices: %s\n", strings.Join(reachableURLs(addr, o.tlsCert != ""), "  "))
				if o.auth == authKey {
					fmt.Fprintf(os.Stderr, "  sign in with the key in %s\n", tildeHome(config.RemoteKeyPath()))
				}
			}

			sig := make(chan os.Signal, 1)
			signal.Notify(sig, os.Interrupt, syscall.SIGTERM)
			select {
			case err := <-d.errc:
				return err
			case <-sig:
				log.Info("shutting down")
			}
			return nil
		},
	}
	f := cmd.Flags()
	f.StringVar(&addr, "addr", config.DefaultAddr, "listen address; 0.0.0.0:7317 for all interfaces (asks to confirm)")
	f.StringVar(&o.auth, "auth", authKey, "beyond loopback: key (sign in with ~/.acc/remote_key) or none")
	f.StringSliceVar(&o.hosts, "allow-host", nil, "host names the dashboard may be reached by, besides IPs and this machine's name")
	f.StringVar(&o.tlsCert, "tls-cert", "", "TLS certificate (PEM) for other machines")
	f.StringVar(&o.tlsKey, "tls-key", "", "TLS private key (PEM)")
	f.BoolVar(&o.yes, "yes", false, "accept listening beyond loopback or without auth without asking")
	return cmd
}

// daemon is a running ACC server, used by `acc serve` and embedded by `acc tui`.
type daemon struct {
	srv    *http.Server
	st     *store.Store
	log    *slog.Logger
	errc   chan error // receives a Serve failure; never receives on clean shutdown
	cancel context.CancelFunc
}

func startDaemon(addr string, log *slog.Logger, o serveOpts) (*daemon, error) {
	if o.exposed(addr) && !o.approved {
		return nil, fmt.Errorf("refusing to listen on %s: use acc serve --addr, which asks to confirm", addr)
	}
	access, err := o.access(addr)
	if err != nil {
		return nil, err
	}
	if err := os.MkdirAll(config.Home(), 0o700); err != nil {
		return nil, err
	}
	token, err := config.EnsureToken()
	if err != nil {
		return nil, err
	}
	// Bind before touching the DB so a second instance fails fast and harmlessly.
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return nil, err
	}
	if o.tlsCert != "" {
		tl, err := newTLSListener(ln, o.tlsCert, o.tlsKey)
		if err != nil {
			ln.Close()
			return nil, err
		}
		ln = tl
	}
	st, err := store.Open(config.DBPath())
	if err != nil {
		ln.Close()
		return nil, err
	}

	n, err := hook.ReplaySpool(func(ev model.Event) error {
		_, err := st.RecordEvent(context.Background(), ev)
		return err
	})
	if err != nil {
		log.Warn("spool replay stopped", "err", err)
	}
	if n > 0 {
		log.Info("replayed spooled events", "count", n)
	}

	api := server.New(st, token, log)
	api.SetAccess(access)
	bg, cancel := context.WithCancel(context.Background())
	api.Start(bg)
	d := &daemon{
		srv: &http.Server{
			Addr:              addr,
			Handler:           api.Handler(),
			ReadHeaderTimeout: 5 * time.Second,
		},
		st:     st,
		log:    log,
		errc:   make(chan error, 1),
		cancel: cancel,
	}
	go func() {
		if err := d.srv.Serve(ln); !errors.Is(err, http.ErrServerClosed) {
			d.errc <- err
		}
	}()
	log.Info("acc serving", "addr", addr, "auth", cmp.Or(o.auth, authKey), "tls", o.tlsCert != "", "db", config.DBPath())
	return d, nil
}

func (d *daemon) Close() {
	d.cancel()
	// SSE streams never finish on their own; don't wait long for them.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	d.srv.Shutdown(ctx)
	d.st.Close()
}

func isLoopback(addr string) bool {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	if host == "localhost" {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func openCmd() *cobra.Command {
	var printOnly bool
	cmd := &cobra.Command{
		Use:   "open",
		Short: "Open the web dashboard in a browser (signs in with the local token)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			tok, err := config.Token()
			if err != nil {
				return fmt.Errorf("no token: start `acc serve` first: %w", err)
			}
			url := config.ServerURL() + "/?token=" + tok
			if printOnly {
				fmt.Println(url)
				return nil
			}
			opener := "xdg-open"
			if runtime.GOOS == "darwin" {
				opener = "open"
			}
			if err := exec.Command(opener, url).Start(); err != nil {
				fmt.Println(url)
			}
			return nil
		},
	}
	cmd.Flags().BoolVar(&printOnly, "print", false, "print the sign-in URL instead of opening it")
	return cmd
}
