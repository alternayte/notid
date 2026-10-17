//go:build interop

package interop_test

import (
	"bytes"
	"encoding/json"
	"io"
	"net/http"
	"net/http/cookiejar"
	"net/url"
	"regexp"
	"testing"
	"time"
)

// user is a signed-in person on a notid installation in the harness.
type user struct {
	t      *testing.T
	origin string
	name   string
	http   *http.Client
}

// signUp creates a person on the installation at origin and signs in through
// the magic link that reaches the mail catcher.
func signUp(t *testing.T, origin string) *user {
	t.Helper()
	jar, _ := cookiejar.New(nil)
	c := client()
	c.Jar = jar
	u := &user{t: t, origin: origin, name: uniqueName("u"), http: c}
	email := u.name + "@example.org"
	u.post("/api/signup", map[string]string{"email": email, "name": u.name}, nil)
	u.post("/api/auth/magic-link/send", map[string]string{"email": email}, nil)
	token := magicToken(t, email)
	u.post("/api/auth/magic-link/verify", map[string]string{"token": token}, nil)
	return u
}

func (u *user) handle() string {
	host, _ := url.Parse(u.origin)
	return u.name + "@" + host.Host
}

func (u *user) post(path string, in, out any) {
	u.t.Helper()
	b, err := json.Marshal(in)
	if err != nil {
		u.t.Fatal(err)
	}
	req, err := http.NewRequest(http.MethodPost, u.origin+path, bytes.NewReader(b))
	if err != nil {
		u.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Origin", u.origin)
	resp, err := u.http.Do(req)
	if err != nil {
		u.t.Fatalf("POST %s: %v", path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 300 {
		u.t.Fatalf("POST %s: %d %.300s", path, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			u.t.Fatalf("POST %s: decode %.300s: %v", path, raw, err)
		}
	}
}

var tokenRE = regexp.MustCompile(`token=([A-Za-z0-9_\-]+)`)

// magicToken reads the newest sign-in link sent to email from the mail catcher.
func magicToken(t *testing.T, email string) string {
	t.Helper()
	var token string
	eventually(t, "a sign-in link for "+email, func() bool {
		var list struct {
			Messages []struct {
				ID string `json:"ID"`
			} `json:"messages"`
		}
		if err := getJSON("http://127.0.0.1:28025/api/v1/search?query="+url.QueryEscape("to:"+email), &list); err != nil || len(list.Messages) == 0 {
			return false
		}
		var msg struct{ Text string }
		if err := getJSON("http://127.0.0.1:28025/api/v1/message/"+list.Messages[0].ID, &msg); err != nil {
			return false
		}
		m := tokenRE.FindStringSubmatch(msg.Text)
		if m == nil {
			return false
		}
		token = m[1]
		return true
	})
	return token
}

func getJSON(u string, out any) error {
	c := client()
	c.Timeout = 10 * time.Second
	resp, err := c.Get(u)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	return json.NewDecoder(resp.Body).Decode(out)
}
