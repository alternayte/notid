package platform

import (
	"encoding/base64"
	"errors"
	"fmt"
	"net/mail"
	"net/netip"
	"net/url"
	"strings"
)

type TLSMode string

const (
	TLSACME TLSMode = "acme"
	TLSOff  TLSMode = "off"
)

type SignupMode string

const (
	SignupInvite SignupMode = "invite"
	SignupOpen   SignupMode = "open"
	SignupClosed SignupMode = "closed"
)

type S3Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
}

type Config struct {
	DatabaseURL string
	AppDomain   string
	// AppOrigin is the browser origin of the app domain, e.g. https://example.org.
	AppOrigin string
	SecretKey []byte
	SMTPURL   *url.URL
	EmailFrom string

	TLS       TLSMode
	ACMEEmail string
	CertDir   string
	// HTTPAddr is the public listener when TLS is off.
	HTTPAddr     string
	WebUpstream  *url.URL
	InternalAddr string

	MediaDir string
	S3       *S3Config

	FederationHTTP bool
	Signup         SignupMode
	// TrustedProxies are the networks of a TLS-terminating proxy in front of the listener.
	TrustedProxies []netip.Prefix
}

// LoadConfig reads config from getenv and reports every missing or unsafe value at once.
func LoadConfig(getenv func(string) string) (Config, error) {
	var errs []error
	get := func(k, def string) string {
		if v := strings.TrimSpace(getenv(k)); v != "" {
			return v
		}
		return def
	}
	required := func(k string) string {
		v := get(k, "")
		if v == "" {
			errs = append(errs, fmt.Errorf("%s is required", k))
		}
		return v
	}

	c := Config{
		DatabaseURL:  required("DATABASE_URL"),
		AppDomain:    required("APP_DOMAIN"),
		EmailFrom:    required("EMAIL_FROM"),
		ACMEEmail:    get("ACME_EMAIL", ""),
		CertDir:      get("CERT_DIR", "/data/certs"),
		HTTPAddr:     get("HTTP_ADDR", ":8080"),
		InternalAddr: get("INTERNAL_ADDR", ":8081"),
	}

	if c.AppDomain != "" && !isBareHost(c.AppDomain) {
		errs = append(errs, fmt.Errorf("APP_DOMAIN must be a bare host name, got %q", c.AppDomain))
	}
	if c.EmailFrom != "" {
		if _, err := mail.ParseAddress(c.EmailFrom); err != nil {
			errs = append(errs, fmt.Errorf("EMAIL_FROM: %w", err))
		}
	}

	if raw := required("SECRET_KEY"); raw != "" {
		key, err := base64.StdEncoding.DecodeString(raw)
		switch {
		case err != nil:
			errs = append(errs, fmt.Errorf("SECRET_KEY must be base64: %w", err))
		case len(key) != 32:
			errs = append(errs, fmt.Errorf("SECRET_KEY must decode to 32 bytes, got %d", len(key)))
		default:
			c.SecretKey = key
		}
	}

	if raw := required("SMTP_URL"); raw != "" {
		u, err := url.Parse(raw)
		if err != nil || (u.Scheme != "smtp" && u.Scheme != "smtps") || u.Host == "" {
			errs = append(errs, fmt.Errorf("SMTP_URL must be smtp:// or smtps:// with a host"))
		} else {
			c.SMTPURL = u
		}
	}

	upstream := get("WEB_UPSTREAM", "http://web:3000")
	if u, err := url.Parse(upstream); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		errs = append(errs, fmt.Errorf("WEB_UPSTREAM must be an http(s) URL, got %q", upstream))
	} else {
		c.WebUpstream = u
	}

	switch m := TLSMode(get("TLS", string(TLSACME))); m {
	case TLSACME, TLSOff:
		c.TLS = m
	default:
		errs = append(errs, fmt.Errorf("TLS must be acme or off, got %q", m))
	}

	switch m := SignupMode(get("SIGNUP", string(SignupInvite))); m {
	case SignupInvite, SignupOpen, SignupClosed:
		c.Signup = m
	default:
		errs = append(errs, fmt.Errorf("SIGNUP must be invite, open or closed, got %q", m))
	}

	c.AppOrigin = "https://" + c.AppDomain
	if raw := get("APP_ORIGIN", ""); raw != "" {
		u, err := url.Parse(raw)
		switch {
		case c.TLS != TLSOff:
			errs = append(errs, errors.New("APP_ORIGIN is allowed only with TLS=off"))
		case err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() != c.AppDomain || u.Path != "" || u.RawQuery != "" || u.User != nil:
			errs = append(errs, fmt.Errorf("APP_ORIGIN must be scheme://APP_DOMAIN[:port], got %q", raw))
		default:
			c.AppOrigin = raw
		}
	}

	for raw := range strings.SplitSeq(get("TRUSTED_PROXIES", ""), ",") {
		if raw = strings.TrimSpace(raw); raw == "" {
			continue
		}
		p, err := netip.ParsePrefix(raw)
		if err != nil {
			errs = append(errs, fmt.Errorf("TRUSTED_PROXIES must be a comma-separated list of CIDR prefixes, got %q", raw))
			continue
		}
		c.TrustedProxies = append(c.TrustedProxies, p.Masked())
	}

	switch v := get("FEDERATION_HTTP", ""); v {
	case "", "0":
	case "1":
		c.FederationHTTP = true
		if c.TLS != TLSOff {
			errs = append(errs, errors.New("FEDERATION_HTTP=1 is allowed only with TLS=off"))
		}
	default:
		errs = append(errs, fmt.Errorf("FEDERATION_HTTP must be 0 or 1, got %q", v))
	}

	s3 := S3Config{
		Endpoint:  get("S3_ENDPOINT", ""),
		Bucket:    get("S3_BUCKET", ""),
		AccessKey: get("S3_ACCESS_KEY", ""),
		SecretKey: get("S3_SECRET_KEY", ""),
	}
	mediaDir := get("MEDIA_DIR", "")
	switch set := countSet(s3.Endpoint, s3.Bucket, s3.AccessKey, s3.SecretKey); {
	case set == 4 && mediaDir != "":
		errs = append(errs, errors.New("set MEDIA_DIR or the S3_* values, not both"))
	case set == 4:
		c.S3 = &s3
	case set > 0:
		errs = append(errs, errors.New("S3_ENDPOINT, S3_BUCKET, S3_ACCESS_KEY and S3_SECRET_KEY must be set together"))
	case mediaDir == "":
		c.MediaDir = "/data/media"
	default:
		c.MediaDir = mediaDir
	}

	return c, errors.Join(errs...)
}

func isBareHost(h string) bool {
	u, err := url.Parse("//" + h)
	return err == nil && u.Host == h && u.Hostname() != "" && u.User == nil
}

func countSet(vs ...string) int {
	n := 0
	for _, v := range vs {
		if v != "" {
			n++
		}
	}
	return n
}
