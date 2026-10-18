package discussions_test

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/riverdriver/riverpgxv5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/pgtest"
)

func TestALinkPostShowsThePreviewOfItsPageAndALinkToAnImageShowsTheImage(t *testing.T) {
	ctx := context.Background()
	pool := pgtest.Pool(t)
	tx, err := pool.Begin(ctx)
	if err != nil {
		t.Fatal(err)
	}
	me := trustedPerson(t, tx)
	c, err := communities.CreateLocal(ctx, tx, local(t), me, "c"+uuid.NewString()[:8], communities.Settings{Title: "Joinery"})
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

	page := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/photo" {
			_, _ = w.Write([]byte("\x89PNG\r\n\x1a\n\x00\x00\x00\rIHDR"))
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte(`<head><meta property="og:title" content="Restoring a lathe"><meta property="og:image" content="/lathe.jpg"></head>`))
	}))
	defer page.Close()

	svc := &discussions.Service{
		Pool: pool, Local: local(t), Markdown: platform.NewMarkdown(), Tuning: platform.DefaultTuning(), HTTP: page.Client(),
		SendPost:       func(context.Context, pgx.Tx, actors.Actor, discussions.Post) error { return nil },
		SendPostUpdate: func(context.Context, pgx.Tx, actors.Actor, discussions.Post) error { return nil },
		ProxyURL:       func(remote string) string { return "/proxy?u=" + remote },
	}
	workers := river.NewWorkers()
	svc.AddWorkers(workers)
	if svc.River, err = river.NewClient(riverpgxv5.New(pool), &river.Config{Workers: workers}); err != nil {
		t.Fatal(err)
	}

	post, err := svc.Post(ctx, me, discussions.PostBody{Community: groupApID, Title: "Lathe", URL: page.URL + "/story"})
	if err != nil {
		t.Fatal(err)
	}
	var queued int
	if err := pool.QueryRow(ctx, `SELECT count(*) FROM river_job WHERE kind = 'link-preview' AND (args->>'post_id')::bigint = $1`, post.ID).Scan(&queued); err != nil || queued != 1 {
		t.Fatalf("queued %d previews: %v", queued, err)
	}
	if err := svc.FetchPreview(ctx, post.ID); err != nil {
		t.Fatal(err)
	}
	thread, err := svc.Thread(ctx, post.Ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	pv := thread.Post.Preview
	if pv == nil || pv.Title != "Restoring a lathe" || pv.ImageSrc != "/proxy?u="+page.URL+"/lathe.jpg" || thread.Post.Image {
		t.Fatalf("post %+v preview %+v", thread.Post, pv)
	}

	if _, err := svc.EditPost(ctx, me, post.Ref, discussions.EditPostBody{Title: "Lathe", URL: page.URL + "/photo"}); err != nil {
		t.Fatal(err)
	}
	if thread, err = svc.Thread(ctx, post.Ref, nil); err != nil || thread.Post.Preview != nil {
		t.Fatalf("the old preview stayed after the link changed: %+v %v", thread.Post.Preview, err)
	}
	if err := svc.FetchPreview(ctx, post.ID); err != nil {
		t.Fatal(err)
	}
	thread, err = svc.Thread(ctx, post.Ref, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !thread.Post.Image || !strings.HasSuffix(thread.Post.ImageSrc, "/photo") || thread.Post.Preview != nil {
		t.Fatalf("post %+v", thread.Post)
	}
}
