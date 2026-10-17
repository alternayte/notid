package platform

import (
	"encoding/base64"
	"strings"
	"testing"
)

func validEnv() map[string]string {
	return map[string]string{
		"DATABASE_URL": "postgres://notid@localhost/notid",
		"APP_DOMAIN":   "example.org",
		"SECRET_KEY":   base64.StdEncoding.EncodeToString(make([]byte, 32)),
		"SMTP_URL":     "smtp://localhost:1025",
		"EMAIL_FROM":   "notid <hello@example.org>",
	}
}

func load(env map[string]string) (Config, error) {
	return LoadConfig(func(k string) string { return env[k] })
}

func TestConfigDefaults(t *testing.T) {
	c, err := load(validEnv())
	if err != nil {
		t.Fatal(err)
	}
	if c.TLS != TLSACME || c.Signup != SignupInvite || c.WebUpstream.String() != "http://web:3000" {
		t.Fatalf("unexpected defaults: %+v", c)
	}
	if len(c.SecretKey) != 32 {
		t.Fatalf("secret key length %d", len(c.SecretKey))
	}
	if c.MediaDir == "" {
		t.Fatal("media dir must default when S3 is not set")
	}
}

func TestConfigRejectsMissingAndUnsafeValues(t *testing.T) {
	cases := map[string]func(map[string]string){
		"missing DATABASE_URL":      func(e map[string]string) { delete(e, "DATABASE_URL") },
		"missing APP_DOMAIN":        func(e map[string]string) { delete(e, "APP_DOMAIN") },
		"missing SMTP_URL":          func(e map[string]string) { delete(e, "SMTP_URL") },
		"missing EMAIL_FROM":        func(e map[string]string) { delete(e, "EMAIL_FROM") },
		"missing SECRET_KEY":        func(e map[string]string) { delete(e, "SECRET_KEY") },
		"short SECRET_KEY":          func(e map[string]string) { e["SECRET_KEY"] = base64.StdEncoding.EncodeToString(make([]byte, 16)) },
		"bad SECRET_KEY":            func(e map[string]string) { e["SECRET_KEY"] = "not base64!" },
		"APP_DOMAIN with scheme":    func(e map[string]string) { e["APP_DOMAIN"] = "https://example.org" },
		"APP_DOMAIN with path":      func(e map[string]string) { e["APP_DOMAIN"] = "example.org/x" },
		"bad TLS":                   func(e map[string]string) { e["TLS"] = "maybe" },
		"bad SIGNUP":                func(e map[string]string) { e["SIGNUP"] = "sometimes" },
		"FEDERATION_HTTP with ACME": func(e map[string]string) { e["FEDERATION_HTTP"] = "1" },
		"partial S3":                func(e map[string]string) { e["S3_BUCKET"] = "b" },
		"MEDIA_DIR and S3":          func(e map[string]string) { setS3(e); e["MEDIA_DIR"] = "/data" },
		"bad SMTP_URL":              func(e map[string]string) { e["SMTP_URL"] = "localhost:25" },
		"bad WEB_UPSTREAM":          func(e map[string]string) { e["WEB_UPSTREAM"] = "web:3000" },
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			env := validEnv()
			mutate(env)
			if _, err := load(env); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestConfigFederationHTTPNeedsTLSOff(t *testing.T) {
	env := validEnv()
	env["TLS"] = "off"
	env["FEDERATION_HTTP"] = "1"
	c, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if !c.FederationHTTP {
		t.Fatal("FEDERATION_HTTP not set")
	}
}

func TestConfigS3(t *testing.T) {
	env := validEnv()
	setS3(env)
	c, err := load(env)
	if err != nil {
		t.Fatal(err)
	}
	if c.S3 == nil || c.MediaDir != "" {
		t.Fatalf("expected S3 only: %+v", c)
	}
}

func TestConfigErrorNamesEveryProblem(t *testing.T) {
	_, err := load(map[string]string{})
	for _, k := range []string{"DATABASE_URL", "APP_DOMAIN", "SECRET_KEY", "SMTP_URL", "EMAIL_FROM"} {
		if err == nil || !strings.Contains(err.Error(), k) {
			t.Fatalf("error %v does not name %s", err, k)
		}
	}
}

func setS3(e map[string]string) {
	e["S3_ENDPOINT"] = "https://s3.example.org"
	e["S3_BUCKET"] = "media"
	e["S3_ACCESS_KEY"] = "a"
	e["S3_SECRET_KEY"] = "s"
}
