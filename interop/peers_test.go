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
			// A post from before the join reaches A only through the outbox.
			before := "Before the join on " + peerName
			c.peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): before, "community_id": c.peer.resolveCommunity(c.url(name)), "body": "older"}, nil)
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
			eventually(t, "A fetches the older post from the "+peerName+" outbox", func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM posts p
					JOIN communities c ON c.id = p.community_id JOIN actors g ON g.id = c.actor_id
					WHERE g.ap_id = '%s' AND p.title = '%s'`, c.url(name), before)) == "1"
			})
		})
	}
}

func TestADirectoryAddFollowsLemmyAndPieFedCommunities(t *testing.T) {
	admin := signUp(t, "http://a.notid.test:8080")
	sql(t, "notid-a-db", fmt.Sprintf(`UPDATE auth_users SET role = 'admin'
		WHERE id = (SELECT user_id FROM actors WHERE local AND name = '%s')`, admin.name))
	for peerName, c := range map[string]struct {
		peer *api
		url  func(string) string
	}{
		"lemmy":  {lemmy(t), func(n string) string { return "http://lemmy.test:8536/c/" + n }},
		"piefed": {piefed(t), func(n string) string { return "http://piefed.test:5000/c/" + n }},
	} {
		t.Run(peerName, func(t *testing.T) {
			c.peer.t, admin.t = t, t
			name := uniqueName("news")
			c.peer.createCommunity(name)
			id := c.peer.resolveCommunity(c.url(name))
			before := "Listed before on " + peerName
			var older postView
			c.peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): before, "community_id": id, "body": "older"}, &older)
			c.peer.call(http.MethodPost, "/comment", map[string]any{bodyKey(peerName): "older reply on " + peerName, "post_id": older.PostView.Post.ID}, nil)

			admin.post("/api/admin/directory", map[string]string{"community": c.url(name), "note": "interop"}, nil)
			siteFollow := fmt.Sprintf(`SELECT coalesce((SELECT m.state FROM memberships m
				JOIN actors p ON p.id = m.actor_id JOIN communities c ON c.id = m.community_id JOIN actors g ON g.id = c.actor_id
				WHERE p.kind = 'service' AND g.ap_id = '%s'), 'none')`, c.url(name))
			eventually(t, peerName+" accepts the site follow", func() bool { return sql(t, "notid-a-db", siteFollow) == "accepted" })
			eventually(t, "A fetches the older post from the "+peerName+" outbox", func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM posts WHERE ap_id = '%s'`, older.PostView.Post.ApID)) == "1"
			})
			// PieFed publishes the post's thread; Lemmy's client API lists its comments.
			eventually(t, "A fetches the older reply on "+peerName, func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM comments m JOIN posts p ON p.id = m.post_id
					WHERE p.ap_id = '%s' AND m.body = 'older reply on %s'`, older.PostView.Post.ApID, peerName)) == "1"
			})

			// A new post arrives through the site follow and shows in All for a signed-out visitor.
			after := "Listed after on " + peerName + " " + uniqueName("")
			c.peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): after, "community_id": id, "body": "newer"}, nil)
			eventually(t, "All on A shows the new "+peerName+" post", func() bool {
				var page struct{ Posts []struct{ Title string } }
				if err := getJSON("http://a.notid.test:8080/api/feed/all", &page); err != nil {
					return false
				}
				for _, p := range page.Posts {
					if p.Title == after {
						return true
					}
				}
				return false
			})

			admin.post("/api/admin/directory/remove", map[string]string{"community": c.url(name)}, nil)
			if got := sql(t, "notid-a-db", siteFollow); got != "none" {
				t.Fatalf("site follow after removal: %s", got)
			}
		})
	}
}

// It runs on B: A's sign-up limit is spent by the other tests.
func TestTheCommunityIndexReadsLemmyAndPieFedAndSearchFindsTheirCommunities(t *testing.T) {
	admin := signUp(t, "http://b.notid.test:8080")
	sql(t, "notid-b-db", fmt.Sprintf(`UPDATE auth_users SET role = 'admin'
		WHERE id = (SELECT user_id FROM actors WHERE local AND name = '%s')`, admin.name))
	for peerName, c := range map[string]struct {
		peer   *api
		server string
	}{
		"lemmy":  {lemmy(t), "lemmy.test:8536"},
		"piefed": {piefed(t), "piefed.test:5000"},
	} {
		t.Run(peerName, func(t *testing.T) {
			c.peer.t, admin.t = t, t
			name := uniqueName("idx")
			c.peer.createCommunity(name)
			admin.post("/api/admin/index-sources", map[string]any{"server": c.server, "on": true}, nil)
			// Search is public and reads only the stored index.
			eventually(t, "Search on B finds "+name+" from the "+peerName+" index", func() bool {
				var out struct {
					Communities []struct{ Handle, Software string }
				}
				if err := getJSON("http://b.notid.test:8080/api/search/index?q="+name, &out); err != nil {
					return false
				}
				return len(out.Communities) == 1 && out.Communities[0].Software == peerName &&
					out.Communities[0].Handle == name+"@"+strings.Split(c.server, ":")[0]
			})
			if got := sql(t, "notid-b-db", fmt.Sprintf(`SELECT software FROM peers WHERE domain = '%s'`, c.server)); got != peerName {
				t.Fatalf("software of %s: %q", c.server, got)
			}
		})
	}
}

// trust promotes a notid user so its votes count and federate.
func trust(t *testing.T, db string, u *user) {
	t.Helper()
	sql(t, db, fmt.Sprintf(`UPDATE actors SET trust = 'trusted', promoted_at = now() WHERE local AND name = '%s'`, u.name))
}

type postView struct {
	PostView struct {
		Post struct {
			ID    int    `json:"id"`
			ApID  string `json:"ap_id"`
			Title string `json:"title"`
			Name  string `json:"name"`
		} `json:"post"`
		Counts struct {
			Score    int `json:"score"`
			Comments int `json:"comments"`
		} `json:"counts"`
	} `json:"post_view"`
}

// peerTitle is the post title field: name on Lemmy, title on PieFed.
func titleKey(peerName string) string {
	if peerName == "lemmy" {
		return "name"
	}
	return "title"
}

func bodyKey(peerName string) string {
	if peerName == "lemmy" {
		return "content"
	}
	return "body"
}

// findPost returns the peer's view of the post with apID, if the peer has it.
func (a *api) findPost(apID string) (postView, bool) {
	a.t.Helper()
	var out struct {
		Object *struct {
			Post *postView `json:"post"`
		}
	}
	req, _ := http.NewRequest(http.MethodGet, a.base+"/resolve_object?q="+apID, nil)
	req.Header.Set("Authorization", "Bearer "+a.jwt)
	resp, err := client().Do(req)
	if err != nil {
		return postView{}, false
	}
	defer resp.Body.Close()
	var raw map[string]json.RawMessage
	if resp.StatusCode != http.StatusOK || json.NewDecoder(resp.Body).Decode(&raw) != nil || raw["post"] == nil {
		return postView{}, false
	}
	var pv struct {
		Post   json.RawMessage `json:"post"`
		Counts json.RawMessage `json:"counts"`
	}
	_ = json.Unmarshal(raw["post"], &pv)
	var v postView
	_ = json.Unmarshal(pv.Post, &v.PostView.Post)
	_ = json.Unmarshal(pv.Counts, &v.PostView.Counts)
	_ = out
	return v, true
}

func (a *api) postCounts(id int) (score, comments int) {
	a.t.Helper()
	var v postView
	a.call(http.MethodGet, fmt.Sprintf("/post?id=%d", id), nil, &v)
	return v.PostView.Counts.Score, v.PostView.Counts.Comments
}

func TestPeersPostCommentAndVoteInACommunityOnA(t *testing.T) {
	name := uniqueName("wood")
	group := notid(t, "notid-a", "community", "create", name)
	member := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", member)
	member.post("/api/communities/follow", map[string]string{"community": group}, nil)

	for peerName, peer := range map[string]*api{"lemmy": lemmy(t), "piefed": piefed(t)} {
		t.Run(peerName, func(t *testing.T) {
			peer.t, member.t = t, t
			id := peer.resolveCommunity(group)
			if peerName == "lemmy" {
				time.Sleep(8 * time.Second)
			}
			peer.follow(id)
			eventually(t, peerName+" follow accepted", func() bool { return peer.subscribed(id) == "Subscribed" })

			// The peer's user posts, comments and votes in the community on A.
			title := "From " + peerName + " " + uniqueName("")
			var created postView
			peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): title, "community_id": id, "body": "hello **A**"}, &created)
			peerPost := created.PostView.Post
			peer.call(http.MethodPost, "/comment", map[string]any{bodyKey(peerName): "a reply", "post_id": peerPost.ID}, nil)
			eventually(t, "A stores the post and the reply from "+peerName, func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT comment_count FROM posts WHERE ap_id = '%s'`, peerPost.ApID)) == "1"
			})

			// A's member votes and replies; the peer sees both through the group's Announce.
			member.post("/api/votes", map[string]any{"object": peerPost.ApID, "value": 1}, nil)
			member.post("/api/comments", map[string]any{"inReplyTo": peerPost.ApID, "body": "welcome"}, nil)
			eventually(t, peerName+" sees A's vote and reply", func() bool {
				score, comments := peer.postCounts(peerPost.ID)
				return score >= 2 && comments == 2
			})

			// A's member posts; the peer receives it as a follower.
			var mine struct{ ApID string }
			member.post("/api/posts", map[string]any{"community": group, "title": "From A " + uniqueName("")}, &mine)
			var seen postView
			eventually(t, peerName+" receives A's post", func() bool {
				var ok bool
				seen, ok = peer.findPost(mine.ApID)
				return ok
			})

			// The peer's user votes on A's post, then takes the vote back.
			score := func() string {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT score FROM posts WHERE ap_id = '%s'`, mine.ApID))
			}
			peer.call(http.MethodPost, "/post/like", map[string]any{"post_id": seen.PostView.Post.ID, "score": -1}, nil)
			eventually(t, "A counts "+peerName+"'s downvote", func() bool { return score() == "-1" })
			peer.call(http.MethodPost, "/post/like", map[string]any{"post_id": seen.PostView.Post.ID, "score": 0}, nil)
			eventually(t, "A drops "+peerName+"'s vote", func() bool { return score() == "0" })
		})
	}
}

func TestNotidUserPostsInLemmyAndPieFedCommunities(t *testing.T) {
	u := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", u)
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
			u.post("/api/communities/follow", map[string]string{"community": c.url(name)}, nil)
			eventually(t, peerName+" accepts the follow", func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT m.state FROM memberships m
					JOIN actors p ON p.id = m.actor_id JOIN communities c ON c.id = m.community_id JOIN actors g ON g.id = c.actor_id
					WHERE p.name = '%s' AND g.ap_id = '%s'`, u.name, c.url(name))) == "accepted"
			})

			var mine struct{ ApID string }
			u.post("/api/posts", map[string]any{"community": c.url(name), "title": "Chisels", "body": "Which **steel**?"}, &mine)
			var pv postView
			eventually(t, peerName+" stores A's post", func() bool {
				var ok bool
				pv, ok = c.peer.findPost(mine.ApID)
				return ok
			})
			u.post("/api/comments", map[string]any{"inReplyTo": mine.ApID, "body": "Answering myself"}, nil)
			u.post("/api/votes", map[string]any{"object": mine.ApID, "value": -1}, nil)
			eventually(t, peerName+" sees A's reply and vote", func() bool {
				_, comments := c.peer.postCounts(pv.PostView.Post.ID)
				score, _ := c.peer.postCounts(pv.PostView.Post.ID)
				return comments == 1 && score <= 0
			})

			// The peer's own post reaches A through the group's Announce.
			cid := c.peer.resolveCommunity(c.url(name))
			var created postView
			c.peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): "Peer post " + uniqueName(""), "community_id": cid}, &created)
			eventually(t, "A receives "+peerName+"'s post", func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM posts WHERE ap_id = '%s'`, created.PostView.Post.ApID)) == "1"
			})
		})
	}
}

func TestInstallationsAAndBShareACommunity(t *testing.T) {
	name := uniqueName("wood")
	group := notid(t, "notid-a", "community", "create", name)
	onA := signUp(t, "http://a.notid.test:8080")
	onB := signUp(t, "http://b.notid.test:8080")
	trust(t, "notid-a-db", onA)
	trust(t, "notid-b-db", onB)

	var m struct{ State string }
	onB.post("/api/communities/follow", map[string]string{"community": group}, &m)
	onA.post("/api/communities/follow", map[string]string{"community": group}, nil)
	eventually(t, "A accepts B's follow", func() bool {
		return sql(t, "notid-b-db", fmt.Sprintf(`SELECT m.state FROM memberships m JOIN actors p ON p.id = m.actor_id
			WHERE p.name = '%s'`, onB.name)) == "accepted"
	})

	var fromB struct{ ApID string }
	onB.post("/api/posts", map[string]any{"community": group, "title": "From B", "body": "planes"}, &fromB)
	eventually(t, "A stores B's post", func() bool {
		return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM posts WHERE ap_id = '%s'`, fromB.ApID)) == "1"
	})
	onA.post("/api/comments", map[string]any{"inReplyTo": fromB.ApID, "body": "nice"}, nil)
	onA.post("/api/votes", map[string]any{"object": fromB.ApID, "value": 1}, nil)
	onB.post("/api/votes", map[string]any{"object": fromB.ApID, "value": 1}, nil)
	for _, db := range []string{"notid-a-db", "notid-b-db"} {
		eventually(t, db+" counts the reply and both votes", func() bool {
			return sql(t, db, fmt.Sprintf(`SELECT comment_count || ' ' || score FROM posts WHERE ap_id = '%s'`, fromB.ApID)) == "1 2"
		})
	}

	var fromA struct{ ApID string }
	onA.post("/api/posts", map[string]any{"community": group, "title": "From A"}, &fromA)
	eventually(t, "B receives A's post", func() bool {
		return sql(t, "notid-b-db", fmt.Sprintf(`SELECT count(*) FROM posts WHERE ap_id = '%s'`, fromA.ApID)) == "1"
	})
}
