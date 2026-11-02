package cli

import (
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
	cmd := &cobra.Command{
		Use:   "serve",
		Short: "Run the ACC daemon (API, live stream and web dashboard)",
		RunE: func(cmd *cobra.Command, _ []string) error {
			log := slog.New(slog.NewTextHandler(os.Stderr, nil))
			d, err := startDaemon(addr, log)
			if err != nil {
				return err
			}
			defer d.Close()
			fmt.Fprintf(os.Stderr, "Dashboard: run `acc open`\n")

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
	cmd.Flags().StringVar(&addr, "addr", config.DefaultAddr, "listen address (loopback only until OIDC lands)")
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

func startDaemon(addr string, log *slog.Logger) (*daemon, error) {
	if !isLoopback(addr) {
		// Hosted mode requires OIDC, which isn't implemented yet.
		return nil, fmt.Errorf("refusing to listen on non-loopback %s: hosted mode (OIDC) is not implemented yet", addr)
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
	log.Info("acc serving", "url", "http://"+addr, "db", config.DBPath())
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
