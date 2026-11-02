package cli

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strings"
	"time"

	"github.com/mattn/go-isatty"

	"github.com/emdzej/acc/internal/config"
	"github.com/emdzej/acc/internal/server"
)

// Auth modes for acc serve.
const (
	authKey  = "key"  // a pre-shared key (~/.acc/remote_key) or the local token
	authNone = "none" // anyone who can reach the port
)

// serveOpts are acc serve's network options. The zero value is the
// loopback-only daemon acc tui embeds.
type serveOpts struct {
	auth            string
	hosts           []string
	tlsCert, tlsKey string
	yes             bool // confirmed with --yes instead of interactively
	approved        bool // the user accepted the exposure (see confirmExposure)
}

// exposed reports whether the options let anyone but this machine's token
// holders in: listening beyond loopback, or no auth.
func (o serveOpts) exposed(addr string) bool { return !isLoopback(addr) || o.auth == authNone }

// access is what the server enforces for these options.
func (o serveOpts) access(addr string) (server.Access, error) {
	a := server.Access{Hosts: o.hosts}
	switch {
	case o.auth == authNone:
		a.NoAuth = true
	case !isLoopback(addr):
		k, err := config.EnsureRemoteKey()
		if err != nil {
			return a, err
		}
		a.RemoteKey = k
	}
	return a, nil
}

// confirmExposure explains what listening on addr gives away and asks the
// user to accept it: typing "yes", or "no auth" without auth. Without a
// terminal, --yes is required.
func confirmExposure(addr string, o *serveOpts) error {
	return confirmFrom(addr, o, os.Stdin, os.Stderr, isatty.IsTerminal(os.Stdin.Fd()))
}

func confirmFrom(addr string, o *serveOpts, in io.Reader, out io.Writer, interactive bool) error {
	none := o.auth == authNone
	tlsOn := o.tlsCert != ""
	var b strings.Builder
	fmt.Fprintf(&b, "\nACC NETWORK ACCESS\n")
	fmt.Fprintf(&b, "  Listening on  %s\n", addr)
	if urls := reachableURLs(addr, tlsOn); len(urls) > 0 {
		fmt.Fprintf(&b, "  Reachable at  %s\n", strings.Join(urls, "  "))
	}
	switch {
	case none:
		fmt.Fprintf(&b, "  Auth          NONE: anyone who can reach this port has full control\n")
	default:
		fmt.Fprintf(&b, "  Auth          pre-shared key (%s, or $ACC_REMOTE_KEY)\n", tildeHome(config.RemoteKeyPath()))
	}
	if tlsOn {
		fmt.Fprintf(&b, "  Transport     TLS for other machines (plain HTTP from this machine only)\n")
	} else if !isLoopback(addr) {
		fmt.Fprintf(&b, "  Transport     plain HTTP: the key and your sessions cross the network unencrypted\n")
	}
	fmt.Fprintf(&b, "\nWhoever gets in can read your sessions and transcripts, send prompts your\n")
	fmt.Fprintf(&b, "agents act on, and start new agent sessions here: that is running code as you.\n")
	fmt.Fprint(out, b.String())

	word := "yes"
	if none {
		word = "no auth"
	}
	if o.yes {
		fmt.Fprintf(out, "Accepted with --yes.\n\n")
		o.approved = true
		return nil
	}
	if !interactive {
		return errors.New("refusing to listen beyond loopback or without auth unattended: confirm in a terminal, or pass --yes")
	}
	fmt.Fprintf(out, "\nType %q to continue: ", word)
	line, err := bufio.NewReader(in).ReadString('\n')
	if err != nil && !errors.Is(err, io.EOF) {
		return err
	}
	if strings.TrimSpace(strings.ToLower(line)) != word {
		return errors.New("not confirmed")
	}
	o.approved = true
	fmt.Fprintln(out)
	return nil
}

// reachableURLs lists the dashboard's URLs on this machine's addresses.
func reachableURLs(addr string, tlsOn bool) []string {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil
	}
	scheme := "http"
	if tlsOn {
		scheme = "https"
	}
	var hosts []string
	if ip := net.ParseIP(host); host != "" && (ip == nil || !ip.IsUnspecified()) {
		hosts = []string{host}
	} else {
		addrs, _ := net.InterfaceAddrs()
		for _, a := range addrs {
			n, ok := a.(*net.IPNet)
			if !ok || n.IP.IsLoopback() || n.IP.IsLinkLocalUnicast() || (n.IP.To4() == nil && host != "::") {
				continue // IPv4 only for 0.0.0.0 and ""; IPv6 too for ::
			}
			hosts = append(hosts, n.IP.String())
		}
	}
	var out []string
	for _, h := range hosts {
		out = append(out, scheme+"://"+net.JoinHostPort(h, port))
	}
	return out
}

func tildeHome(p string) string {
	if home, err := os.UserHomeDir(); err == nil && strings.HasPrefix(p, home+"/") {
		return "~" + p[len(home):]
	}
	return p
}

// tlsListener serves TLS to other machines and plain HTTP to this one, on
// one port: local clients (hooks, acc CLI, the TUI) keep using
// http://127.0.0.1, while nothing crosses the network unencrypted.
type tlsListener struct {
	net.Listener
	cfg   *tls.Config
	conns chan net.Conn
	errc  chan error
}

func newTLSListener(ln net.Listener, certFile, keyFile string) (*tlsListener, error) {
	cert, err := tls.LoadX509KeyPair(certFile, keyFile)
	if err != nil {
		return nil, fmt.Errorf("TLS certificate: %w", err)
	}
	l := &tlsListener{Listener: ln, cfg: &tls.Config{Certificates: []tls.Certificate{cert}, MinVersion: tls.VersionTLS12},
		conns: make(chan net.Conn), errc: make(chan error, 1)}
	go l.loop()
	return l, nil
}

func (l *tlsListener) loop() {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			l.errc <- err
			return
		}
		go l.sniff(c) // a slow client must not hold up the others
	}
}

// sniff looks at the first byte: 0x16 starts a TLS handshake.
func (l *tlsListener) sniff(c net.Conn) {
	c.SetReadDeadline(time.Now().Add(10 * time.Second))
	br := bufio.NewReader(c)
	first, err := br.Peek(1)
	c.SetReadDeadline(time.Time{})
	if err != nil {
		c.Close()
		return
	}
	pc := &peekedConn{Conn: c, r: br}
	switch {
	case first[0] == 0x16:
		l.conns <- tls.Server(pc, l.cfg)
	case isLoopbackConn(c):
		l.conns <- pc
	default:
		io.WriteString(c, "HTTP/1.1 400 Bad Request\r\nConnection: close\r\nContent-Type: text/plain\r\n\r\nACC requires https from other machines\n")
		c.Close()
	}
}

func (l *tlsListener) Accept() (net.Conn, error) {
	select {
	case c := <-l.conns:
		return c, nil
	case err := <-l.errc:
		l.errc <- err // keep reporting it
		return nil, err
	}
}

type peekedConn struct {
	net.Conn
	r *bufio.Reader
}

func (c *peekedConn) Read(p []byte) (int, error) { return c.r.Read(p) }

func isLoopbackConn(c net.Conn) bool {
	a, ok := c.RemoteAddr().(*net.TCPAddr)
	return ok && a.IP.IsLoopback()
}
