//go:build interop

package interop_test

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os/exec"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
)

// api is a JSON API client for Lemmy's v3 or PieFed's alpha API.
type api struct {
	t    *testing.T
	base string
	jwt  string
}

func (a *api) call(method, path string, in, out any) {
	a.t.Helper()
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			a.t.Fatal(err)
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.base+path, body)
	if err != nil {
		a.t.Fatal(err)
	}
	req.Header.Set("Content-Type", "application/json")
	if a.jwt != "" {
		req.Header.Set("Authorization", "Bearer "+a.jwt)
	}
	resp, err := client().Do(req)
	if err != nil {
		a.t.Fatalf("%s %s: %v", method, path, err)
	}
	defer resp.Body.Close()
	raw, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != http.StatusOK {
		a.t.Fatalf("%s %s: %d %.300s", method, path, resp.StatusCode, raw)
	}
	if out != nil {
		if err := json.Unmarshal(raw, out); err != nil {
			a.t.Fatalf("%s %s: decode %.300s: %v", method, path, raw, err)
		}
	}
}

func lemmy(t *testing.T) *api {
	a := &api{t: t, base: "http://lemmy.test:8536/api/v3"}
	var login struct{ JWT string }
	a.call(http.MethodPost, "/user/login", map[string]string{"username_or_email": "admin", "password": "lemmylemmy"}, &login)
	a.jwt = login.JWT
	return a
}

func piefed(t *testing.T) *api {
	a := &api{t: t, base: "http://piefed.test:5000/api/alpha"}
	var login struct{ JWT string }
	a.call(http.MethodPost, "/user/login", map[string]string{"username": "admin", "password": "lemmylemmy"}, &login)
	a.jwt = login.JWT
	return a
}

type communityView struct {
	CommunityView struct {
		Community struct {
			ID      int    `json:"id"`
			ActorID string `json:"actor_id"`
		} `json:"community"`
		Subscribed string `json:"subscribed"`
	} `json:"community_view"`
}

// resolveCommunity makes the peer fetch a community. PieFed looks up
// WebFinger over https only, so the harness resolves by actor URL.
func (a *api) resolveCommunity(actorURL string) int {
	a.t.Helper()
	var out struct {
		Community struct {
			Community struct {
				ID int `json:"id"`
			} `json:"community"`
		} `json:"community"`
	}
	a.call(http.MethodGet, "/resolve_object?q="+actorURL, nil, &out)
	return out.Community.Community.ID
}

func (a *api) follow(id int) {
	a.t.Helper()
	a.call(http.MethodPost, "/community/follow", map[string]any{"community_id": id, "follow": true}, nil)
}

func (a *api) subscribed(id int) string {
	a.t.Helper()
	var v communityView
	a.call(http.MethodGet, fmt.Sprintf("/community?id=%d", id), nil, &v)
	return v.CommunityView.Subscribed
}

// notid runs the notid command inside a harness installation.
func notid(t *testing.T, service string, args ...string) string {
	t.Helper()
	cmd := exec.Command("docker", append([]string{"compose", "-f", "compose.yaml", "exec", "-T", service, "notid"}, args...)...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("notid %v on %s: %v\n%s", args, service, err, out)
	}
	return strings.TrimSpace(string(out))
}

// sql runs one query in a harness database and returns the trimmed output.
func sql(t *testing.T, service, query string) string {
	t.Helper()
	out, err := exec.Command("docker", "compose", "-f", "compose.yaml", "exec", "-T", service,
		"psql", "-U", "notid", "-d", "notid", "-tAc", query).CombinedOutput()
	if err != nil {
		t.Fatalf("sql on %s: %v\n%s", service, err, out)
	}
	return strings.TrimSpace(string(out))
}

func uniqueName(prefix string) string {
	return prefix + strings.ReplaceAll(uuid.NewString()[:8], "-", "")
}

// eventually polls cond until it holds. Lemmy batches outgoing activities, so
// the wait covers a few of its send rounds.
func eventually(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(2 * time.Second)
	}
	t.Fatalf("timed out: %s", what)
}

func TestLemmyAndPieFedFollowACommunityOnA(t *testing.T) {
	name := uniqueName("wood")
	group := notid(t, "notid-a", "community", "create", name, "-title", "Woodworking")
	if group != "http://a.notid.test:8080/c/"+name {
		t.Fatalf("group AP ID %q", group)
	}

	for peerName, peer := range map[string]*api{"lemmy": lemmy(t), "piefed": piefed(t)} {
		t.Run(peerName, func(t *testing.T) {
			peer.t = t
			id := peer.resolveCommunity(group)
			if id == 0 {
				t.Fatal("peer did not resolve the community")
			}
			if peerName == "lemmy" {
				// Lemmy starts a send worker for a newly seen server within
				// 5 s, at the newest queued activity; a Follow queued before
				// that start is never sent.
				time.Sleep(8 * time.Second)
			}
			peer.follow(id)
			eventually(t, peerName+" shows the follow as accepted", func() bool {
				return peer.subscribed(id) == "Subscribed"
			})
			// PieFed shows Subscribed before the Accept arrives, so A's side decides.
			host := strings.TrimPrefix(strings.Split(peer.base, "/api")[0], "http://")
			eventually(t, "A holds an accepted membership from "+peerName, func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM memberships m
					JOIN actors a ON a.id = m.actor_id JOIN communities c ON c.id = m.community_id
					WHERE c.name = '%s' AND m.state = 'accepted' AND a.ap_id LIKE 'http://%s/%%'`, name, host)) == "1"
			})
		})
	}
}

// createCommunity makes a community on the peer as its admin.
func (a *api) createCommunity(name string) {
	a.t.Helper()
	a.call(http.MethodPost, "/community", map[string]any{"name": name, "title": "Interop " + name}, nil)
}

func TestNotidUserFollowsLemmyAndPieFedCommunities(t *testing.T) {
	u := signUp(t, "http://a.notid.test:8080")
	for peerName, c := range map[string]struct {
		peer *api
		url  func(string) string
	}{
		"lemmy":  {lemmy(t), func(n string) string { return "http://lemmy.test:8536/c/" + n }},
		"piefed": {piefed(t), func(n string) string { return "http://piefed.test:5000/c/" + n }},
	} {
		t.Run(peerName, func(t *testing.T) {
			c.peer.t, u.t = t, t
			name := uniqueName("tools")
			c.peer.createCommunity(name)
			var m struct{ Community, State string }
			u.post("/api/communities/follow", map[string]string{"community": c.url(name)}, &m)
			if m.Community != c.url(name) || m.State != "pending" {
				t.Fatalf("follow answer %+v", m)
			}
			eventually(t, peerName+" accepts the follow", func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT m.state FROM memberships m
					JOIN actors p ON p.id = m.actor_id JOIN communities c ON c.id = m.community_id JOIN actors g ON g.id = c.actor_id
					WHERE p.name = '%s' AND g.ap_id = '%s'`, u.name, c.url(name))) == "accepted"
			})
		})
	}
}
