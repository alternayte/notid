package discussions_test

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

func local(t *testing.T) actors.Local {
	s, err := platform.NewSealer(bytes.Repeat([]byte{1}, 32))
	if err != nil {
		t.Fatal(err)
	}
	return actors.Local{AppOrigin: "https://notid.test", AppDomain: "notid.test", Sealer: s}
}

func trustedPerson(t *testing.T, tx pgx.Tx) actors.Actor {
	t.Helper()
	ctx := context.Background()
	id := uuid.New()
	if _, err := tx.Exec(ctx,
		`INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at) VALUES ($1, $2, $2, '', '', now(), now())`,
		id, id.String()+"@example.org"); err != nil {
		t.Fatal(err)
	}
	a, err := local(t).CreatePerson(ctx, tx, id, "p"+id.String()[:8], "")
	if err != nil {
		t.Fatal(err)
	}
	trusted := "trusted"
	if _, err := tx.Exec(ctx, `UPDATE actors SET trust = $2 WHERE id = $1`, a.ID, trusted); err != nil {
		t.Fatal(err)
	}
	a.Trust = &trusted
	return a
}

func TestPostReplySaveAndReadThroughTheAPI(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	me := trustedPerson(t, tx)
	name := "c" + uuid.NewString()[:8]
	c, err := communities.CreateLocal(ctx, tx, local(t), me, name, communities.Settings{Title: "Joinery"})
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(ctx); err != nil {
		t.Fatal(err)
	}
	var groupApID string
	if err := pool.QueryRow(ctx, `SELECT ap_id FROM actors WHERE id = $1`, c.ActorID).Scan(&groupApID); err != nil {
		t.Fatal(err)
	}
	tuning := platform.DefaultTuning()
	tuning.PostsPerPage = 2
	svc := &discussions.Service{
		Pool: pool, Local: local(t), Markdown: platform.NewMarkdown(), Tuning: tuning,
		SendPost:      func(context.Context, pgx.Tx, actors.Actor, discussions.Post) error { return nil },
		SendComment:   func(context.Context, pgx.Tx, actors.Actor, discussions.Comment) error { return nil },
		SendVote:      func(context.Context, pgx.Tx, actors.Actor, int64, string, int16, string, string) error { return nil },
		NewActivityID: func(string) (string, error) { return "https://notid.test/a/" + uuid.NewString(), nil },
	}
	mux := http.NewServeMux()
	svc.Register(platform.NewAPI(mux, "test"))
	send := func(method, path string, body, out any) {
		t.Helper()
		var r *http.Request
		if body != nil {
			b, _ := json.Marshal(body)
			r = httptest.NewRequest(method, path, bytes.NewReader(b))
			r.Header.Set("Content-Type", "application/json")
		} else {
			r = httptest.NewRequest(method, path, nil)
		}
		w := httptest.NewRecorder()
		mux.ServeHTTP(w, r.WithContext(accounts.WithActor(r.Context(), &me)))
		if w.Code >= 300 {
			t.Fatalf("%s %s: %d %s", method, path, w.Code, w.Body)
		}
		if out != nil {
			if err := json.Unmarshal(w.Body.Bytes(), out); err != nil {
				t.Fatal(err)
			}
		}
	}

	var posts [3]discussions.Created
	for i := range posts {
		send(http.MethodPost, "/api/posts", map[string]any{"community": groupApID, "title": "Dovetails", "body": "Cut **tails** first"}, &posts[i])
	}
	var top, reply discussions.Created
	send(http.MethodPost, "/api/comments", map[string]any{"inReplyTo": posts[0].ApID, "body": "Pins first"}, &top)
	send(http.MethodPost, "/api/comments", map[string]any{"inReplyTo": top.ApID, "body": "Why?"}, &reply)
	send(http.MethodPost, "/api/votes", map[string]any{"object": posts[0].ApID, "value": 1}, nil)
	send(http.MethodPost, "/api/saves", map[string]any{"object": reply.ApID, "saved": true}, nil)

	var thread discussions.ThreadView
	send(http.MethodGet, "/api/posts/"+posts[0].Ref, nil, &thread)
	p := thread.Post
	if p.Community != name || p.Score != 1 || p.Viewer.Vote != 1 || !p.Viewer.VoteCounted || p.Comments != 2 ||
		p.BodyHTML != "<p>Cut <strong>tails</strong> first</p>\n" || p.Author.Handle != me.Name {
		t.Fatalf("post %+v", p)
	}
	if len(thread.Comments) != 2 || thread.Comments[0].ParentRef != "" || thread.Comments[1].ParentRef != top.Ref ||
		!thread.Comments[1].Viewer.Saved || thread.Comments[0].Viewer.Saved {
		t.Fatalf("comments %+v", thread.Comments)
	}

	var page discussions.PostPage
	send(http.MethodGet, "/api/communities/"+name+"/posts", nil, &page)
	if len(page.Posts) != 2 || page.Posts[0].Ref != posts[2].Ref || page.Next == "" {
		t.Fatalf("first page %+v", page)
	}
	send(http.MethodGet, "/api/communities/"+name+"/posts?before="+page.Next, nil, &page)
	if len(page.Posts) != 1 || page.Posts[0].Ref != posts[0].Ref || page.Next != "" {
		t.Fatalf("second page %+v", page)
	}

	var saved discussions.SavedPage
	send(http.MethodGet, "/api/me/saved", nil, &saved)
	if len(saved.Items) != 1 || saved.Items[0].Comment == nil || saved.Items[0].Comment.Ref != reply.Ref || saved.Items[0].Post.Ref != posts[0].Ref {
		t.Fatalf("saved %+v", saved)
	}
	send(http.MethodPost, "/api/saves", map[string]any{"object": reply.ApID, "saved": false}, nil)
	send(http.MethodGet, "/api/me/saved", nil, &saved)
	if len(saved.Items) != 0 {
		t.Fatalf("unsaved item still listed: %+v", saved)
	}
}
