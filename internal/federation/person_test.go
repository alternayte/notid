package federation

import (
	"context"
	"encoding/json"
	"net/http"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
)

func TestInboundPersonDeleteRemovesWhatTheRemotePersonLeftHere(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)
	g := testGroup(t, s)
	receive(t, s, peer, follow(peer, g))

	pageID := "http://" + peer.host + "/post/" + uuid.NewString()
	receive(t, s, peer, map[string]any{
		"id": "http://" + peer.host + "/activities/" + uuid.NewString(), "type": "Create", "actor": peer.bob(),
		"to": []string{g.apID, public}, "audience": g.apID,
		"object": map[string]any{
			"id": pageID, "type": "Page", "attributedTo": peer.bob(),
			"to": []string{g.apID, public}, "audience": g.apID,
			"name": "A post", "content": "<p>text</p>", "mediaType": "text/html",
		},
	})
	noteID := "http://" + peer.host + "/note/" + uuid.NewString()
	receive(t, s, peer, map[string]any{
		"id": "http://" + peer.host + "/activities/" + uuid.NewString(), "type": "Create", "actor": peer.bob(),
		"to": []string{public}, "cc": []string{g.apID}, "audience": g.apID,
		"object": map[string]any{
			"id": noteID, "type": "Note", "attributedTo": peer.bob(),
			"to": []string{public}, "cc": []string{g.apID}, "audience": g.apID,
			"inReplyTo": pageID, "content": "<p>reply</p>", "mediaType": "text/html",
		},
	})

	stranger, err := actors.UpsertRemote(ctx, s.Pool, actors.Remote{
		ApID: "https://elsewhere.test/u/eve", Kind: "person", Domain: "elsewhere.test", Name: "eve",
	})
	if err != nil {
		t.Fatal(err)
	}
	del := func(object string) map[string]any {
		return map[string]any{
			"id": "http://" + peer.host + "/activities/" + uuid.NewString(), "type": "Delete",
			"actor": peer.bob(), "to": []string{public}, "object": object,
		}
	}
	// Nobody deletes another person's account.
	if w := post(s, peer.signedBy(peer.key, "/inbox", del(stranger.ApID))); w.Code != http.StatusForbidden {
		t.Fatalf("deleting another person: %d %s", w.Code, w.Body)
	}
	receive(t, s, peer, del(peer.bob()))

	var deleted bool
	if err := s.Pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM actors WHERE ap_id = $1`, peer.bob()).Scan(&deleted); err != nil {
		t.Fatal(err)
	}
	if !deleted {
		t.Fatal("the actor is not marked deleted")
	}
	var title, body string
	if err := s.Pool.QueryRow(ctx, `SELECT title, body FROM posts WHERE ap_id = $1`, pageID).Scan(&title, &body); err != nil {
		t.Fatal(err)
	}
	if title != "" || body != "" {
		t.Fatalf("the post kept %q %q", title, body)
	}
	var gone bool
	if err := s.Pool.QueryRow(ctx, `SELECT deleted_at IS NOT NULL FROM comments WHERE ap_id = $1`, noteID).Scan(&gone); err != nil || !gone {
		t.Fatalf("the comment stays: %v", err)
	}
	var memberships int
	if err := s.Pool.QueryRow(ctx, `SELECT count(*) FROM memberships m JOIN actors a ON a.id = m.actor_id WHERE a.ap_id = $1`, peer.bob()).Scan(&memberships); err != nil {
		t.Fatal(err)
	}
	if memberships != 0 {
		t.Fatalf("%d memberships left", memberships)
	}
}

func TestAccountDeletionTellsFollowersAndRemoteCommunities(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)

	person := localPerson(t, s, "leaver")
	remote := func(apID, name, kind string) actors.Actor {
		a, err := actors.UpsertRemote(ctx, s.Pool, actors.Remote{
			ApID: apID, Kind: kind, Domain: peer.host, Name: name, Inbox: apID + "/inbox",
		})
		if err != nil {
			t.Fatal(err)
		}
		return a
	}
	follower := remote("http://"+peer.host+"/u/fan", "fan", "person")
	group := remote("http://"+peer.host+"/c/wood", "wood", "group")

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	mine, err := communities.CreateLocal(ctx, tx, s.Local, person, "leave"+uuid.NewString()[:6], communities.Settings{Title: "Leaving"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state) VALUES ($1, $2, 'accepted')`, mine.ID, follower.ID); err != nil {
		t.Fatal(err)
	}
	var remoteID int64
	if err := tx.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, 'wood') RETURNING id`, group.ID, peer.host).Scan(&remoteID); err != nil {
		t.Fatal(err)
	}
	followApID := "http://" + peer.host + "/activities/follow/" + uuid.NewString()
	if _, err := tx.Exec(ctx, `INSERT INTO memberships (community_id, actor_id, state, follow_ap_id) VALUES ($1, $2, 'accepted', $3)`,
		remoteID, person.ID, followApID); err != nil {
		t.Fatal(err)
	}
	if _, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		CommunityID: mine.ID, Author: person, Title: "Bye", Body: "leaving", Published: time.Now(),
	}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}

	// The deletion's transaction, as DeleteAccount runs it.
	tx, err = s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	s.SendPersonDelete(ctx, tx, person)

	sent := map[string][]map[string]any{}
	for i, r := range peer.received {
		var act map[string]any
		if err := json.Unmarshal(peer.bodies[i], &act); err != nil {
			t.Fatalf("%s: %v", r.URL.Path, err)
		}
		sent[r.URL.Path] = append(sent[r.URL.Path], act)
	}
	if n := len(sent["/u/fan/inbox"]); n != 1 {
		t.Fatalf("the follower got %d activities: %v", n, sent)
	}
	toFollower := sent["/u/fan/inbox"][0]
	if toFollower["type"] != "Delete" || toFollower["actor"] != person.ApID {
		t.Fatalf("follower got %v", toFollower)
	}
	object, _ := toFollower["object"].(map[string]any)
	if object["id"] != person.ApID {
		t.Fatalf("the follower was not told which person: %v", toFollower)
	}

	toGroup := sent["/c/wood/inbox"]
	if len(toGroup) != 2 {
		t.Fatalf("the group got %d activities: %v", len(toGroup), toGroup)
	}
	byType := map[string]map[string]any{}
	for _, a := range toGroup {
		byType[a["type"].(string)] = a
	}
	if byType["Delete"] == nil || byType["Undo"] == nil {
		t.Fatalf("the group got %v", byType)
	}
	inner, _ := byType["Undo"]["object"].(map[string]any)
	if inner["type"] != "Follow" || inner["id"] != followApID || inner["object"] != group.ApID {
		t.Fatalf("undo %v", byType["Undo"])
	}
}
