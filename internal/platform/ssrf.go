package platform

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"time"
)

// Guard refuses outbound requests to private, loopback, link-local and other
// non-public addresses, at request time and again at dial time. Every outbound
// request in notid goes through Client.
type Guard struct {
	// AllowHTTP permits plain http. Only the interop harness sets it.
	AllowHTTP bool
	Timeout   time.Duration
	MaxBytes  int64
	Resolver  Resolver
}

// Resolver is the part of net.Resolver the guard uses. Tests replace it.
type Resolver interface {
	LookupNetIP(ctx context.Context, network, host string) ([]netip.Addr, error)
}

func NewGuard(allowHTTP bool, t Tuning) *Guard {
	return &Guard{
		AllowHTTP: allowHTTP,
		Timeout:   t.FederationTimeout,
		MaxBytes:  t.MaxFetchBytes,
		Resolver:  net.DefaultResolver,
	}
}

// CheckURL refuses a URL whose scheme or resolved addresses the guard forbids.
func (g *Guard) CheckURL(ctx context.Context, raw string) (*url.URL, error) {
	u, err := url.Parse(raw)
	if err != nil {
		return nil, fmt.Errorf("parse url: %w", err)
	}
	if err := g.checkScheme(u); err != nil {
		return nil, err
	}
	if err := g.checkHost(ctx, u.Hostname()); err != nil {
		return nil, err
	}
	return u, nil
}

func (g *Guard) checkScheme(u *url.URL) error {
	if u.Scheme == "https" || (g.AllowHTTP && u.Scheme == "http") {
		return nil
	}
	if u.Scheme == "http" {
		return errors.New("plain http is refused; set FEDERATION_HTTP only for the interop harness")
	}
	return fmt.Errorf("refused scheme %q", u.Scheme)
}

func (g *Guard) checkHost(ctx context.Context, host string) error {
	if host == "" {
		return errors.New("url has no host")
	}
	ips, err := g.lookup(ctx, host)
	if err != nil {
		return err
	}
	for _, ip := range ips {
		if !addressAllowed(ip) {
			return fmt.Errorf("refuse %s: %s is not a public address", host, ip)
		}
	}
	return nil
}

func (g *Guard) lookup(ctx context.Context, host string) ([]netip.Addr, error) {
	ips, err := g.Resolver.LookupNetIP(ctx, "ip", host)
	if err != nil {
		return nil, fmt.Errorf("resolve %s: %w", host, err)
	}
	if len(ips) == 0 {
		return nil, fmt.Errorf("resolve %s: no addresses", host)
	}
	return ips, nil
}

// Client returns an HTTP client that enforces the guard on every request and
// redirect, caps the response body and reaches only vetted addresses.
func (g *Guard) Client() *http.Client {
	return &http.Client{
		Timeout:   g.Timeout,
		Transport: &guardTransport{g: g, next: &http.Transport{DialContext: g.dial}},
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 3 {
				return errors.New("too many redirects")
			}
			_, err := g.CheckURL(req.Context(), req.URL.String())
			return err
		},
	}
}

type guardTransport struct {
	g    *Guard
	next http.RoundTripper
}

func (t *guardTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	if err := t.g.checkScheme(r.URL); err != nil {
		return nil, err
	}
	resp, err := t.next.RoundTrip(r)
	if err != nil {
		return nil, err
	}
	resp.Body = &cappedBody{body: resp.Body, left: t.g.MaxBytes}
	return resp, nil
}

// dial resolves the host itself and reaches one of its vetted addresses by IP,
// so a second DNS answer cannot swap in a private address after the check.
func (g *Guard) dial(ctx context.Context, network, addr string) (net.Conn, error) {
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		return nil, fmt.Errorf("split %q: %w", addr, err)
	}
	ips, err := g.lookup(ctx, host)
	if err != nil {
		return nil, err
	}
	for _, ip := range ips {
		if !addressAllowed(ip) {
			return nil, fmt.Errorf("refuse %s: %s is not a public address", host, ip)
		}
	}
	d := &net.Dialer{Timeout: g.Timeout}
	var lastErr error
	for _, ip := range ips {
		conn, err := d.DialContext(ctx, network, net.JoinHostPort(ip.String(), port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	return nil, fmt.Errorf("dial %s: %w", host, lastErr)
}

// addressAllowed reports whether ip is a public unicast address.
func addressAllowed(ip netip.Addr) bool {
	ip = ip.Unmap()
	if !ip.IsValid() || !ip.IsGlobalUnicast() || ip.IsPrivate() {
		return false
	}
	return !isReserved(ip)
}

// isReserved covers the non-public ranges that IsPrivate does not.
func isReserved(ip netip.Addr) bool {
	if ip.Is4() {
		b := ip.As4()
		switch {
		case b[0] == 0: // 0.0.0.0/8
			return true
		case b[0] == 100 && b[1]&0xc0 == 64: // 100.64.0.0/10 shared address space
			return true
		case b[0] == 192 && b[1] == 0 && (b[2] == 0 || b[2] == 2): // 192.0.0.0/24, 192.0.2.0/24
			return true
		case b[0] == 198 && b[1]&0xfe == 18: // 198.18.0.0/15 benchmarking
			return true
		case b[0] == 198 && b[1] == 51 && b[2] == 100: // 198.51.100.0/24 documentation
			return true
		case b[0] == 203 && b[1] == 0 && b[2] == 113: // 203.0.113.0/24 documentation
			return true
		case b[0] >= 240: // 240.0.0.0/4 reserved and broadcast
			return true
		}
		return false
	}
	b := ip.As16()
	switch {
	case b[0] == 0x20 && b[1] == 0x01 && b[2] == 0x0d && b[3] == 0xb8: // 2001:db8::/32 documentation
		return true
	case b[0] == 0x01 && b[1] == 0x00 && b[2] == 0 && b[3] == 0 &&
		b[4] == 0 && b[5] == 0 && b[6] == 0 && b[7] == 0: // 100::/64 discard
		return true
	}
	return false
}

// cappedBody fails the read once a response exceeds the configured cap.
type cappedBody struct {
	body io.ReadCloser
	left int64
}

func (b *cappedBody) Read(p []byte) (int, error) {
	if b.left <= 0 {
		return 0, errors.New("response exceeds the size cap")
	}
	if int64(len(p)) > b.left {
		p = p[:b.left]
	}
	n, err := b.body.Read(p)
	b.left -= int64(n)
	return n, err
}

func (b *cappedBody) Close() error { return b.body.Close() }
