package federation

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/moderation"
	"github.com/alternayte/notid/internal/moderation/moderationdb"
)

func TestInboundFlagFromARemotePersonReachesTheModeratorQueue(t *testing.T) {
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

	flagID := "http://" + peer.host + "/activities/" + uuid.NewString()
	receive(t, s, peer, map[string]any{
		"id": flagID, "type": "Flag", "actor": peer.bob(), "object": pageID,
		"to": []string{g.apID}, "audience": g.apID, "summary": "This is spam",
	})

	var communityID, reporterID, postID int64
	var reason, flagApID, reporterName string
	if err := s.Pool.QueryRow(ctx, `SELECT r.community_id, r.reporter_id, r.post_id, r.reason, flag_ap_id,
			a.name
		FROM reports r JOIN actors a ON a.id = r.reporter_id
		WHERE r.flag_ap_id = $1`, flagID).Scan(&communityID, &reporterID, &postID, &reason, &flagApID, &reporterName); err != nil {
		t.Fatalf("report: %v", err)
	}
	if reason != "This is spam" || flagApID != flagID {
		t.Fatalf("reason %q flag %q", reason, flagApID)
	}
	var bobID int64
	if err := s.Pool.QueryRow(ctx, `SELECT id FROM actors WHERE ap_id = $1`, peer.bob()).Scan(&bobID); err != nil {
		t.Fatal(err)
	}
	if reporterID != bobID {
		t.Fatalf("reporter %d, want the remote person %d", reporterID, bobID)
	}

	// The moderators read it through the queue they use.
	rows, err := moderationdb.New(s.Pool).ReportQueue(ctx, moderationdb.ReportQueueParams{CommunityID: communityID, Lim: 100})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Reason != "This is spam" || rows[0].ReporterName != "bob" {
		t.Fatalf("queue %+v", rows)
	}

	// A report is private: the group announces nothing.
	for _, j := range jobs(t, s, "deliver", "activity,type", "Announce") {
		var a DeliverArgs
		if err := json.Unmarshal(j.EncodedArgs, &a); err != nil {
			t.Fatal(err)
		}
		if strings.Contains(string(a.Activity), flagID) {
			t.Fatalf("announced the flag: %s", a.Activity)
		}
	}
}

func TestReportingInARemoteCommunitySendsAFlagAndALocalReportStays(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)

	bob, err := actors.UpsertRemote(ctx, s.Pool, actors.Remote{
		ApID: peer.bob(), Kind: "person", Domain: peer.host, Name: "bob",
		Inbox: peer.bob() + "/inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	group, err := actors.UpsertRemote(ctx, s.Pool, actors.Remote{
		ApID: "https://" + peer.host + "/c/wood", Kind: "group", Domain: peer.host, Name: "wood",
		Inbox: "https://" + peer.host + "/c/wood/inbox",
	})
	if err != nil {
		t.Fatal(err)
	}
	remote := community(t, s, group, peer.host, "wood", bob)

	reporter := localPerson(t, s, "reporter")
	svc := &moderation.Service{Pool: s.Pool, Local: s.Local, Tuning: s.Tuning, Logger: s.Log, SendFlag: s.SendFlag}
	reason := "Off topic " + uuid.NewString()

	if _, err := svc.File(ctx, reporter, remote, reason); err != nil {
		t.Fatalf("file: %v", err)
	}
	var flagApID string
	if err := s.Pool.QueryRow(ctx, `SELECT coalesce(flag_ap_id, '') FROM reports WHERE reason = $1`, reason).Scan(&flagApID); err != nil {
		t.Fatal(err)
	}
	if flagApID == "" {
		t.Fatal("the report keeps no flag AP ID")
	}
	queued := jobs(t, s, "deliver", "activity,id", flagApID)
	if len(queued) != 1 {
		t.Fatalf("flag jobs: %d", len(queued))
	}
	var args DeliverArgs
	if err := json.Unmarshal(queued[0].EncodedArgs, &args); err != nil {
		t.Fatal(err)
	}
	if args.Inbox != group.Inbox {
		t.Fatalf("inbox %q, want the group inbox %q", args.Inbox, group.Inbox)
	}
	var sent struct {
		Actor    string `json:"actor"`
		Audience string `json:"audience"`
		Object   string `json:"object"`
		Summary  string `json:"summary"`
		Content  string `json:"content"`
	}
	if err := json.Unmarshal(args.Activity, &sent); err != nil {
		t.Fatal(err)
	}
	if sent.Actor != reporter.ApID || sent.Audience != group.ApID || sent.Object != remote || sent.Summary != reason || sent.Content != reason {
		t.Fatalf("flag %+v", sent)
	}

	// A report in a local community never leaves the installation.
	owner := localPerson(t, s, "owner")
	name := "l" + uuid.NewString()[:8]
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	c, err := communities.CreateLocal(ctx, tx, s.Local, owner, name, communities.Settings{Title: "Local"})
	if err != nil {
		t.Fatal(err)
	}
	post, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		ApID: "https://" + s.AppDomain + "/posts/" + uuid.NewString(), CommunityID: c.ID,
		Author: reporter, Title: "Mine", Body: "body", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	localReason := "Their complaint " + uuid.NewString()
	if _, err := svc.File(ctx, bob, post.ApID, localReason); err != nil {
		t.Fatalf("local report: %v", err)
	}
	if n := len(jobs(t, s, "deliver", "activity,summary", localReason)); n != 0 {
		t.Fatalf("a local report queued %d flags", n)
	}
	var localFlag *string
	if err := s.Pool.QueryRow(ctx, `SELECT flag_ap_id FROM reports WHERE reason = $1`, localReason).Scan(&localFlag); err != nil {
		t.Fatal(err)
	}
	if localFlag != nil {
		t.Fatalf("a local report took a flag AP ID: %q", *localFlag)
	}
}

func localPerson(t *testing.T, s *Server, name string) actors.Actor {
	t.Helper()
	ctx := context.Background()
	userID := uuid.New()
	if _, err := s.Pool.Exec(ctx,
		`INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at) VALUES ($1, $2, $2, '', '', now(), now())`,
		userID, userID.String()+"@example.org"); err != nil {
		t.Fatal(err)
	}
	a, err := s.Local.CreatePerson(ctx, s.Pool, userID, name+userID.String()[:6], "")
	if err != nil {
		t.Fatal(err)
	}
	trusted := "trusted"
	if _, err := s.Pool.Exec(ctx, `UPDATE actors SET trust = $2 WHERE id = $1`, a.ID, trusted); err != nil {
		t.Fatal(err)
	}
	a.Trust = &trusted
	return a
}

// community stores a remote community with one post by author and returns the
// post's AP ID.
func community(t *testing.T, s *Server, group actors.Actor, domain, name string, author actors.Actor) string {
	t.Helper()
	ctx := context.Background()
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	var communityID int64
	if err := tx.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, $2, $3) RETURNING id`,
		group.ID, domain, name).Scan(&communityID); err != nil {
		t.Fatal(err)
	}
	post, _, err := discussions.InsertPost(ctx, tx, discussions.NewPost{
		ApID: "https://" + domain + "/post/" + uuid.NewString(), CommunityID: communityID,
		Author: author, Title: "Theirs", Body: "body", Published: time.Now(),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	return post.ApID
}
