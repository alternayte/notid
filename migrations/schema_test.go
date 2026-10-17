package migrations_test

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/alternayte/notid/internal/platform/pgtest"
)

func TestModerationLogIsAppendOnly(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	var actor int64
	if err := tx.QueryRow(ctx, `INSERT INTO actors (ap_id, kind, local, domain, name) VALUES ('https://r.test/u/m', 'person', false, 'r.test', 'm') RETURNING id`).Scan(&actor); err != nil {
		t.Fatal(err)
	}
	if _, err := tx.Exec(ctx, `INSERT INTO moderation_log (moderator_id, action) VALUES ($1, 'site_ban')`, actor); err != nil {
		t.Fatal(err)
	}
	for _, stmt := range []string{
		`UPDATE moderation_log SET reason = 'x'`,
		`DELETE FROM moderation_log`,
	} {
		if _, err := tx.Exec(ctx, "SAVEPOINT s"); err != nil {
			t.Fatal(err)
		}
		_, err := tx.Exec(ctx, stmt)
		if err == nil || !strings.Contains(err.Error(), "append-only") {
			t.Fatalf("%s: got %v", stmt, err)
		}
		if _, err := tx.Exec(ctx, "ROLLBACK TO SAVEPOINT s"); err != nil {
			t.Fatal(err)
		}
	}
}

func TestPostSortAtCannotBeInTheFuture(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	var actor, group, community int64
	mustScan(t, tx.QueryRow(ctx, `INSERT INTO actors (ap_id, kind, local, domain, name) VALUES ('https://r.test/u/a', 'person', false, 'r.test', 'a') RETURNING id`), &actor)
	mustScan(t, tx.QueryRow(ctx, `INSERT INTO actors (ap_id, kind, local, domain, name) VALUES ('https://r.test/c/g', 'group', false, 'r.test', 'g') RETURNING id`), &group)
	mustScan(t, tx.QueryRow(ctx, `INSERT INTO communities (actor_id, domain, name) VALUES ($1, 'r.test', 'g') RETURNING id`, group), &community)
	future := time.Now().Add(48 * time.Hour)
	_, err := tx.Exec(ctx, `INSERT INTO posts (ap_id, ref, local, community_id, author_id, title, published, received_at, sort_at)
		VALUES ('https://r.test/post/1', 'aaaaaaaaaaaa', false, $1, $2, 't', $3, now(), $3)`, community, actor, future)
	if err == nil {
		t.Fatal("a post with sort_at after received_at was accepted")
	}
}

type scanner interface{ Scan(...any) error }

func mustScan(t *testing.T, row scanner, dst ...any) {
	t.Helper()
	if err := row.Scan(dst...); err != nil {
		t.Fatal(err)
	}
}
