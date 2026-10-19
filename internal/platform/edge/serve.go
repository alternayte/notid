package edge

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"

	"github.com/caddyserver/certmagic"
	"golang.org/x/sync/errgroup"

	"github.com/alternayte/notid/internal/platform"
)

// Serve runs the public listeners and the internal API listener until ctx ends.
func Serve(ctx context.Context, cfg platform.Config, t platform.Tuning, hosts Hosts, public, internal http.Handler, log *slog.Logger) error {
	servers := []*http.Server{newServer(cfg.InternalAddr, internal, t)}
	var tlsServer *http.Server

	switch cfg.TLS {
	case platform.TLSOff:
		// A proxy in front terminates TLS; the browser still reaches the app over https.
		if strings.HasPrefix(cfg.AppOrigin, "https://") {
			public = withHSTS(public)
		}
		servers = append(servers, newServer(cfg.HTTPAddr, public, t))
	case platform.TLSACME:
		magic, issuer := newCertMagic(cfg, hosts, log)
		tlsServer = newServer(":443", withHSTS(public), t)
		tlsServer.TLSConfig = magic.TLSConfig()
		tlsServer.TLSConfig.NextProtos = append([]string{"h2", "http/1.1"}, tlsServer.TLSConfig.NextProtos...)
		servers = append(servers, newServer(":80", issuer.HTTPChallengeHandler(redirectHTTPS(hosts)), t), tlsServer)
	default:
		return fmt.Errorf("unknown TLS mode %q", cfg.TLS)
	}

	g, gctx := errgroup.WithContext(ctx)
	for _, s := range servers {
		g.Go(func() error {
			log.Info("listening", "addr", s.Addr)
			var err error
			if s == tlsServer {
				err = s.ListenAndServeTLS("", "")
			} else {
				err = s.ListenAndServe()
			}
			if errors.Is(err, http.ErrServerClosed) {
				return nil
			}
			return fmt.Errorf("listener %s: %w", s.Addr, err)
		})
	}
	g.Go(func() error {
		<-gctx.Done()
		sctx, cancel := context.WithTimeout(context.Background(), t.ShutdownTimeout)
		defer cancel()
		var errs []error
		for _, s := range servers {
			errs = append(errs, s.Shutdown(sctx))
		}
		return errors.Join(errs...)
	})
	return g.Wait()
}

func newServer(addr string, h http.Handler, t platform.Tuning) *http.Server {
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: t.ReadHeaderTimeout,
		ReadTimeout:       t.ReadTimeout,
		WriteTimeout:      t.WriteTimeout,
		IdleTimeout:       t.IdleTimeout,
		MaxHeaderBytes:    t.MaxHeaderBytes,
	}
}

func newCertMagic(cfg platform.Config, hosts Hosts, log *slog.Logger) (*certmagic.Config, *certmagic.ACMEIssuer) {
	var magic *certmagic.Config
	cache := certmagic.NewCache(certmagic.CacheOptions{
		GetConfigForCert: func(certmagic.Certificate) (*certmagic.Config, error) { return magic, nil },
		Logger:           certmagic.Default.Logger,
	})
	magic = certmagic.New(cache, certmagic.Config{
		Storage:  &certmagic.FileStorage{Path: cfg.CertDir},
		OnDemand: &certmagic.OnDemandConfig{DecisionFunc: hosts.CertDecision},
	})
	issuer := certmagic.NewACMEIssuer(magic, certmagic.ACMEIssuer{
		CA:     certmagic.LetsEncryptProductionCA,
		Email:  cfg.ACMEEmail,
		Agreed: true,
	})
	magic.Issuers = []certmagic.Issuer{issuer}
	log.Info("on-demand TLS enabled", "app_domain", cfg.AppDomain)
	return magic, issuer
}

func redirectHTTPS(hosts Hosts) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		host := requestHost(r)
		state, err := hosts.State(r.Context(), host)
		if err != nil || state != HostServed {
			http.Error(w, "misdirected request", http.StatusMisdirectedRequest)
			return
		}
		http.Redirect(w, r, "https://"+host+r.URL.RequestURI(), http.StatusPermanentRedirect)
	})
}
