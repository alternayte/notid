package federation

import (
	"context"
	"testing"
)

func TestTheIndexReadsTheCommunityListOfALemmyPeerAndReplacesItOnTheNextRead(t *testing.T) {
	ctx := context.Background()
	peer := newFakePeer(t)
	s := testServer(t, peer)
	// The peer answers on plain HTTP, as in the interop harness.
	s.AppOrigin = "http://notid.test"
	base := "http://" + peer.host
	listed := func(name string, active int, extra map[string]any) map[string]any {
		c := map[string]any{"actor_id": base + "/c/" + name, "name": name, "title": "The " + name, "visibility": "Public"}
		for k, v := range extra {
			c[k] = v
		}
		return map[string]any{"community": c, "counts": map[string]any{"users_active_month": active}}
	}
	peer.docs = map[string]any{
		"/.well-known/nodeinfo": map[string]any{"links": []any{
			map[string]any{"rel": "http://nodeinfo.diaspora.software/ns/schema/2.1", "href": base + "/nodeinfo/2.1"},
		}},
		"/nodeinfo/2.1": map[string]any{"software": map[string]any{"name": "lemmy", "version": "0.19.20"}},
		"/api/v3/community/list": map[string]any{"communities": []any{
			listed("technology", 900, map[string]any{"description": "## News\n\n![logo](https://x/y.gif) and **[views](https://x)**\n---\n::: spoiler rules\n1. Be kind\n:::"}),
			listed("after_dark", 40, map[string]any{"nsfw": true}),
			listed("gone", 5, map[string]any{"deleted": true}),
			listed("members_only", 5, map[string]any{"visibility": "LocalOnly"}),
			// A list names only the server's own communities; another host's is left out.
			map[string]any{"community": map[string]any{"actor_id": "http://elsewhere.test/c/spoof", "name": "spoof"}},
		}},
	}
	if _, err := s.Pool.Exec(ctx, `INSERT INTO peers (domain) VALUES ($1)`, peer.host); err != nil {
		t.Fatal(err)
	}

	read := func() {
		t.Helper()
		if _, err := s.Pool.Exec(ctx, `DELETE FROM river_job WHERE kind = 'index'`); err != nil {
			t.Fatal(err)
		}
		if err := (&indexAllWorker{s: s}).Work(ctx, nil); err != nil {
			t.Fatal(err)
		}
		queued := jobs(t, s, "index", "server", peer.host)
		if len(queued) != 1 {
			t.Fatalf("queued %d reads of %s", len(queued), peer.host)
		}
		if err := (&indexWorker{s: s}).Work(ctx, job[IndexArgs](t, queued[0])); err != nil {
			t.Fatal(err)
		}
	}
	read()

	type row struct {
		name, description string
		active            int32
		nsfw              bool
	}
	stored := func() map[string]row {
		t.Helper()
		rows, err := s.Pool.Query(ctx, `SELECT name, description, active_month, nsfw FROM community_index WHERE server = $1`, peer.host)
		if err != nil {
			t.Fatal(err)
		}
		defer rows.Close()
		out := map[string]row{}
		for rows.Next() {
			var r row
			if err := rows.Scan(&r.name, &r.description, &r.active, &r.nsfw); err != nil {
				t.Fatal(err)
			}
			out[r.name] = r
		}
		return out
	}
	got := stored()
	if len(got) != 2 || got["technology"] != (row{"technology", "News and views Be kind", 900, false}) || !got["after_dark"].nsfw {
		t.Fatalf("index holds %+v", got)
	}
	var software string
	var indexed bool
	if err := s.Pool.QueryRow(ctx, `SELECT software, indexed_at IS NOT NULL FROM peers WHERE domain = $1`, peer.host).Scan(&software, &indexed); err != nil {
		t.Fatal(err)
	}
	if software != "lemmy" || !indexed {
		t.Fatalf("peer software %q, indexed %v", software, indexed)
	}

	// The next read drops a community the server no longer lists.
	peer.docs["/api/v3/community/list"] = map[string]any{"communities": []any{listed("technology", 950, nil)}}
	read()
	if got := stored(); len(got) != 1 || got["technology"].active != 950 {
		t.Fatalf("after the second read the index holds %+v", got)
	}
}
