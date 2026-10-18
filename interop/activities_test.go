//go:build interop

package interop_test

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"io"
	"mime/multipart"
	"net/http"
	"net/textproto"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// INTEROP_RECORD=1 writes each activity the tests wait for into the
// federation package's fixtures.
const fixtureDir = "../internal/federation/testdata/peers"

// inboxActivity waits for the newest activity in db's inbox queue that matches
// where, a SQL condition on the activity a, and returns it as indented JSON.
func inboxActivity(t *testing.T, db, what, where string) string {
	t.Helper()
	var out string
	eventually(t, db+" receives "+what, func() bool {
		out = sql(t, db, `SELECT jsonb_pretty(a) FROM (SELECT id, args->'activity' AS a FROM river_job WHERE kind = 'inbox') j
			WHERE `+where+` ORDER BY id DESC LIMIT 1`)
		return out != ""
	})
	return out
}

// record keeps a received activity as a fixture when recording is on.
func record(t *testing.T, name, activity string) {
	t.Helper()
	if os.Getenv("INTEROP_RECORD") == "" {
		return
	}
	if err := os.WriteFile(filepath.Join(fixtureDir, name+".json"), []byte(activity+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
}

// of matches an activity of type kind, sent directly or inside a group's Announce.
func of(kind string) string {
	return fmt.Sprintf(`(a->>'type' = '%[1]s' OR (a->>'type' = 'Announce' AND a->'object'->>'type' = '%[1]s'))`, kind)
}

// personDelete matches a Delete of the person at apID, linked or embedded.
func personDelete(apID string) string {
	return fmt.Sprintf(`a->>'type' = 'Delete' AND (a->>'object' = '%[1]s' OR a->'object'->>'id' = '%[1]s')`, apID)
}

func naming(apID string) string {
	return fmt.Sprintf(`a::text LIKE '%%"%s"%%'`, apID)
}

func pngImage(t *testing.T) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, 64, 48))
	for x := range 64 {
		for y := range 48 {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 5), B: 120, A: 255})
		}
	}
	var b bytes.Buffer
	if err := png.Encode(&b, img); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}

// upload stores an image on u's installation and returns its URL.
func (u *user) upload() string {
	u.t.Helper()
	var form bytes.Buffer
	mw := multipart.NewWriter(&form)
	h := textproto.MIMEHeader{}
	h.Set("Content-Disposition", `form-data; name="file"; filename="bench.png"`)
	h.Set("Content-Type", "image/png")
	part, _ := mw.CreatePart(h)
	_, _ = part.Write(pngImage(u.t))
	mw.Close()
	var up struct{ URL string }
	u.do(http.MethodPost, "/api/media", mw.FormDataContentType(), io.Reader(&form), &up)
	return up.URL
}

// createCommunity makes a community owned by u and returns its group AP ID.
func (u *user) createCommunity(name string) string {
	u.t.Helper()
	u.post("/api/communities", map[string]any{"name": name, "title": "Interop " + name, "description": "", "rules": []string{}, "theme": map[string]string{}, "appealsContact": "", "nsfw": false, "holdNewPosts": false}, nil)
	return u.origin + "/c/" + name
}

// personID returns the peer's id for a person it can fetch.
func (a *api) personID(apID string) int {
	a.t.Helper()
	var out struct {
		Person struct {
			Person struct {
				ID int `json:"id"`
			} `json:"person"`
		} `json:"person"`
	}
	a.call(http.MethodGet, "/resolve_object?q="+apID, nil, &out)
	return out.Person.Person.ID
}

// commentID is the id of the comment a peer returns from a create.
type commentView struct {
	CommentView struct {
		Comment struct {
			ID   int    `json:"id"`
			ApID string `json:"ap_id"`
		} `json:"comment"`
	} `json:"comment_view"`
}

func TestPeersChangeAndReportTheirContentInACommunityOnA(t *testing.T) {
	owner := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", owner)
	group := owner.createCommunity(uniqueName("wood"))
	photo := owner.upload()

	for peerName, peer := range map[string]*api{"lemmy": lemmy(t), "piefed": piefed(t)} {
		t.Run(peerName, func(t *testing.T) {
			peer.t, owner.t = t, t
			id := peer.resolveCommunity(group)
			if peerName == "lemmy" {
				time.Sleep(8 * time.Second)
			}
			peer.follow(id)
			eventually(t, peerName+" follow accepted", func() bool { return peer.subscribed(id) == "Subscribed" })
			stored := func(apID, column string) string {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT %s FROM posts WHERE ap_id = '%s'`, column, apID))
			}

			var img postView
			peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): "My bench", "community_id": id, "url": photo}, &img)
			eventually(t, "A shows "+peerName+"'s image post as an image", func() bool { return stored(img.PostView.Post.ApID, "url_image") == "t" })
			record(t, peerName+"-create-image-page", inboxActivity(t, "notid-a-db", "the image post", of("Create")+" AND "+naming(img.PostView.Post.ApID)))

			var link postView
			peer.call(http.MethodPost, "/post", map[string]any{titleKey(peerName): "How this site federates", "community_id": id, "url": "http://a.notid.test:8080/about/federation"}, &link)
			eventually(t, "A stores "+peerName+"'s link post", func() bool {
				return stored(link.PostView.Post.ApID, "url || ' ' || url_image") == "http://a.notid.test:8080/about/federation false"
			})
			record(t, peerName+"-create-link-page", inboxActivity(t, "notid-a-db", "the link post", of("Create")+" AND "+naming(link.PostView.Post.ApID)))

			peer.call(http.MethodPut, "/post", map[string]any{"post_id": link.PostView.Post.ID, titleKey(peerName): "How this site federates", "url": "http://a.notid.test:8080/about/federation", "body": "Edited on " + peerName}, nil)
			eventually(t, "A applies "+peerName+"'s post edit", func() bool { return stored(link.PostView.Post.ApID, "body") == "Edited on "+peerName })
			record(t, peerName+"-update-page", inboxActivity(t, "notid-a-db", "the post edit", of("Update")+" AND "+naming(link.PostView.Post.ApID)))

			var c commentView
			peer.call(http.MethodPost, "/comment", map[string]any{bodyKey(peerName): "first thought", "post_id": link.PostView.Post.ID}, &c)
			comment := c.CommentView.Comment
			peer.call(http.MethodPut, "/comment", map[string]any{"comment_id": comment.ID, bodyKey(peerName): "second thought"}, nil)
			commentBody := func() string {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT body || '|' || (deleted_at IS NOT NULL) FROM comments WHERE ap_id = '%s'`, comment.ApID))
			}
			eventually(t, "A applies "+peerName+"'s comment edit", func() bool { return commentBody() == "second thought|false" })
			record(t, peerName+"-update-note", inboxActivity(t, "notid-a-db", "the comment edit", of("Update")+" AND "+naming(comment.ApID)))

			peer.call(http.MethodPost, "/comment/delete", map[string]any{"comment_id": comment.ID, "deleted": true}, nil)
			eventually(t, "A deletes "+peerName+"'s comment", func() bool { return commentBody() == "|true" })
			record(t, peerName+"-delete-note", inboxActivity(t, "notid-a-db", "the comment delete", of("Delete")+" AND "+naming(comment.ApID)))

			peer.call(http.MethodPost, "/post/report", map[string]any{"post_id": img.PostView.Post.ID, "reason": "not a bench"}, nil)
			eventually(t, "A files "+peerName+"'s report", func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT r.reason FROM reports r JOIN posts p ON p.id = r.post_id WHERE p.ap_id = '%s'`, img.PostView.Post.ApID)) == "not a bench"
			})
			record(t, peerName+"-flag", inboxActivity(t, "notid-a-db", "the report", of("Flag")+" AND "+naming(img.PostView.Post.ApID)))

			peer.call(http.MethodPost, "/post/delete", map[string]any{"post_id": img.PostView.Post.ID, "deleted": true}, nil)
			eventually(t, "A deletes "+peerName+"'s post", func() bool { return stored(img.PostView.Post.ApID, "deleted_at IS NOT NULL") == "t" })
			record(t, peerName+"-delete-page", inboxActivity(t, "notid-a-db", "the post delete", of("Delete")+" AND "+naming(img.PostView.Post.ApID)))
		})
	}
}

func TestPeerModeratorsActOnANotidMember(t *testing.T) {
	u := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", u)
	actor := "http://a.notid.test:8080/u/" + u.name
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
			group := c.url(name)
			c.peer.createCommunity(name)
			u.post("/api/communities/follow", map[string]string{"community": group}, nil)
			membership := func() string {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT m.state || ' ' || m.role FROM memberships m
					JOIN actors p ON p.id = m.actor_id JOIN communities c ON c.id = m.community_id JOIN actors g ON g.id = c.actor_id
					WHERE p.name = '%s' AND g.ap_id = '%s'`, u.name, group))
			}
			eventually(t, peerName+" accepts the follow", func() bool { return strings.HasPrefix(membership(), "accepted") })
			cid := c.peer.resolveCommunity(group)
			var mine struct{ ApID string }
			u.post("/api/posts", map[string]any{"community": group, "title": "Mine to lock"}, &mine)
			var pv postView
			eventually(t, peerName+" stores A's post", func() bool {
				var ok bool
				pv, ok = c.peer.findPost(mine.ApID)
				return ok
			})
			pid := c.peer.personID(actor)

			c.peer.call(http.MethodPut, "/community", map[string]any{"community_id": cid, "title": "Renamed on " + peerName}, nil)
			eventually(t, "A shows the new title from "+peerName, func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT c.title FROM communities c JOIN actors g ON g.id = c.actor_id WHERE g.ap_id = '%s'`, group)) == "Renamed on "+peerName
			})
			record(t, peerName+"-update-group", inboxActivity(t, "notid-a-db", "the group update", of("Update")+" AND "+naming(group)+` AND a::text LIKE '%"Group"%'`))

			c.peer.call(http.MethodPost, "/post/lock", map[string]any{"post_id": pv.PostView.Post.ID, "locked": true}, nil)
			eventually(t, "A locks the post from "+peerName, func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT locked FROM posts WHERE ap_id = '%s'`, mine.ApID)) == "t"
			})
			record(t, peerName+"-lock", inboxActivity(t, "notid-a-db", "the lock", of("Lock")+" AND "+naming(mine.ApID)))

			c.peer.call(http.MethodPost, "/community/mod", map[string]any{"community_id": cid, "person_id": pid, "added": true}, nil)
			eventually(t, "A makes the member a moderator from "+peerName, func() bool { return membership() == "accepted moderator" })
			record(t, peerName+"-add-moderator", inboxActivity(t, "notid-a-db", "the moderator add", of("Add")+" AND "+naming(actor)))
			c.peer.call(http.MethodPost, "/community/mod", map[string]any{"community_id": cid, "person_id": pid, "added": false}, nil)
			eventually(t, "A makes the moderator a member from "+peerName, func() bool { return membership() == "accepted member" })
			record(t, peerName+"-remove-moderator", inboxActivity(t, "notid-a-db", "the moderator removal", of("Remove")+" AND "+naming(actor)))

			if peerName == "lemmy" {
				c.peer.call(http.MethodPost, "/community/ban_user", map[string]any{"community_id": cid, "person_id": pid, "ban": true, "reason": "interop ban"}, nil)
			} else {
				c.peer.call(http.MethodPost, "/community/moderate/ban", map[string]any{"community_id": cid, "user_id": pid, "reason": "interop ban", "permanent": true}, nil)
			}
			eventually(t, "A records the ban from "+peerName, func() bool {
				return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM bans b JOIN actors p ON p.id = b.actor_id
					JOIN communities c ON c.id = b.community_id JOIN actors g ON g.id = c.actor_id
					WHERE p.name = '%s' AND g.ap_id = '%s'`, u.name, group)) == "1"
			})
			record(t, peerName+"-block", inboxActivity(t, "notid-a-db", "the ban", of("Block")+" AND "+naming(actor)))
		})
	}
}

func TestALemmyAccountDeletionRemovesItsContentOnA(t *testing.T) {
	admin := lemmy(t)
	admin.call(http.MethodPut, "/site", map[string]any{"registration_mode": "Open"}, nil)
	name := uniqueName("gone")
	var reg struct{ JWT string }
	admin.call(http.MethodPost, "/user/register", map[string]any{"username": name, "password": "lemmylemmy", "password_verify": "lemmylemmy"}, &reg)
	if reg.JWT == "" {
		t.Fatal("Lemmy registered the user without a session")
	}
	peer := &api{t: t, base: admin.base, jwt: reg.JWT}

	owner := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", owner)
	group := owner.createCommunity(uniqueName("wood"))
	id := peer.resolveCommunity(group)
	time.Sleep(8 * time.Second)
	peer.follow(id)
	eventually(t, "lemmy follow accepted", func() bool { return peer.subscribed(id) == "Subscribed" })
	var p postView
	peer.call(http.MethodPost, "/post", map[string]any{"name": "Leaving soon", "community_id": id, "body": "bye"}, &p)
	eventually(t, "A stores the post", func() bool {
		return sql(t, "notid-a-db", fmt.Sprintf(`SELECT count(*) FROM posts WHERE ap_id = '%s'`, p.PostView.Post.ApID)) == "1"
	})

	peer.call(http.MethodPost, "/user/delete_account", map[string]any{"password": "lemmylemmy", "delete_content": true}, nil)
	person := "http://lemmy.test:8536/u/" + name
	eventually(t, "A forgets the person and their post", func() bool {
		return sql(t, "notid-a-db", fmt.Sprintf(`SELECT (a.deleted_at IS NOT NULL) || ' ' || (p.deleted_at IS NOT NULL)
			FROM actors a JOIN posts p ON p.author_id = a.id WHERE a.ap_id = '%s'`, person)) == "true true"
	})
	record(t, "lemmy-delete-person", inboxActivity(t, "notid-a-db", "the account deletion", personDelete(person)))
}

func TestNotidGroupActionsReachAMemberOnB(t *testing.T) {
	owner := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", owner)
	name := uniqueName("wood")
	group := owner.createCommunity(name)
	onB := signUp(t, "http://b.notid.test:8080")
	trust(t, "notid-b-db", onB)
	onB.post("/api/communities/follow", map[string]string{"community": group}, nil)
	roleOnA := func() string {
		return sql(t, "notid-a-db", fmt.Sprintf(`SELECT m.state || ' ' || m.role FROM memberships m JOIN actors p ON p.id = m.actor_id
			JOIN communities c ON c.id = m.community_id WHERE p.name = '%s' AND c.name = '%s'`, onB.name, name))
	}
	roleOnB := func() string {
		return sql(t, "notid-b-db", fmt.Sprintf(`SELECT m.state || ' ' || m.role FROM memberships m JOIN actors p ON p.id = m.actor_id
			JOIN communities c ON c.id = m.community_id JOIN actors g ON g.id = c.actor_id WHERE p.name = '%s' AND g.ap_id = '%s'`, onB.name, group))
	}
	eventually(t, "A accepts B's follow", func() bool { return roleOnB() == "accepted member" })
	member := onB.name + "@b.notid.test"
	memberApID := "http://b.notid.test:8080/u/" + onB.name

	var fromB struct{ ApID, Ref string }
	onB.post("/api/posts", map[string]any{"community": group, "title": "From B", "body": "planes"}, &fromB)
	postOn := func(db, column string) string {
		return sql(t, db, fmt.Sprintf(`SELECT %s FROM posts WHERE ap_id = '%s'`, column, fromB.ApID))
	}
	eventually(t, "A stores B's post", func() bool { return postOn("notid-a-db", "count(*)") == "1" })

	owner.do(http.MethodPut, "/api/communities/"+name, "application/json", map[string]any{"title": "Renamed on A", "description": "", "rules": []string{}, "theme": map[string]string{}, "appealsContact": "", "nsfw": false, "holdNewPosts": false}, nil)
	eventually(t, "B shows the new title", func() bool {
		return sql(t, "notid-b-db", fmt.Sprintf(`SELECT c.title FROM communities c JOIN actors g ON g.id = c.actor_id WHERE g.ap_id = '%s'`, group)) == "Renamed on A"
	})
	record(t, "notid-update-group", inboxActivity(t, "notid-b-db", "the group update", of("Update")+" AND "+naming(group)+` AND a::text LIKE '%"Group"%'`))

	owner.post("/api/moderation/lock", map[string]any{"object": fromB.ApID, "locked": true}, nil)
	eventually(t, "B locks its post", func() bool { return postOn("notid-b-db", "locked") == "t" })
	record(t, "notid-lock", inboxActivity(t, "notid-b-db", "the lock", of("Lock")+" AND "+naming(fromB.ApID)))

	owner.post("/api/communities/"+name+"/moderators", map[string]any{"actor": member, "role": "moderator"}, nil)
	eventually(t, "B shows its member as a moderator", func() bool { return roleOnB() == "accepted moderator" })
	record(t, "notid-add-moderator", inboxActivity(t, "notid-b-db", "the moderator add", of("Add")+" AND "+naming(memberApID)))
	owner.post("/api/communities/"+name+"/moderators", map[string]any{"actor": member, "role": "member"}, nil)
	eventually(t, "B shows the moderator as a member", func() bool { return roleOnB() == "accepted member" })
	record(t, "notid-remove-moderator", inboxActivity(t, "notid-b-db", "the moderator removal", of("Remove")+" AND "+naming(memberApID)))

	var fromA struct{ ApID string }
	owner.post("/api/posts", map[string]any{"community": group, "title": "From A"}, &fromA)
	eventually(t, "B receives A's post", func() bool {
		return sql(t, "notid-b-db", fmt.Sprintf(`SELECT count(*) FROM posts WHERE ap_id = '%s'`, fromA.ApID)) == "1"
	})
	onB.post("/api/reports", map[string]any{"object": fromA.ApID, "reason": "from B"}, nil)
	eventually(t, "A files B's report", func() bool {
		return sql(t, "notid-a-db", fmt.Sprintf(`SELECT r.reason FROM reports r JOIN posts p ON p.id = r.post_id WHERE p.ap_id = '%s'`, fromA.ApID)) == "from B"
	})
	record(t, "notid-flag", inboxActivity(t, "notid-a-db", "the report", of("Flag")+" AND "+naming(fromA.ApID)))

	onB.do(http.MethodPut, "/api/posts/"+fromB.Ref, "application/json", map[string]any{"title": "From B", "body": "planes, edited", "url": "", "nsfw": false}, nil)
	eventually(t, "A applies B's edit", func() bool { return postOn("notid-a-db", "body") == "planes, edited" })
	record(t, "notid-update-page", inboxActivity(t, "notid-a-db", "the post edit", of("Update")+" AND "+naming(fromB.ApID)))

	onB.do(http.MethodDelete, "/api/posts/"+fromB.Ref, "", nil, nil)
	eventually(t, "A deletes B's post", func() bool { return postOn("notid-a-db", "deleted_at IS NOT NULL") == "t" })
	record(t, "notid-delete-page", inboxActivity(t, "notid-a-db", "the post delete", of("Delete")+" AND "+naming(fromB.ApID)))

	owner.post("/api/moderation/ban", map[string]any{"community": name, "actor": member, "reason": "interop ban"}, nil)
	eventually(t, "B records the ban", func() bool {
		return sql(t, "notid-b-db", fmt.Sprintf(`SELECT count(*) FROM bans b JOIN actors p ON p.id = b.actor_id WHERE p.name = '%s'`, onB.name)) == "1"
	})
	record(t, "notid-block", inboxActivity(t, "notid-b-db", "the ban", of("Block")+" AND "+naming(memberApID)))
	if roleOnA() != "" {
		t.Fatalf("A kept the banned member: %q", roleOnA())
	}

	onB.do(http.MethodDelete, "/api/me", "", nil, nil)
	eventually(t, "A forgets B's person", func() bool {
		return sql(t, "notid-a-db", fmt.Sprintf(`SELECT deleted_at IS NOT NULL FROM actors WHERE ap_id = '%s'`, memberApID)) == "t"
	})
	record(t, "notid-delete-person", inboxActivity(t, "notid-a-db", "the account deletion", personDelete(memberApID)))
}
