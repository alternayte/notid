package actors_test

import (
	"bytes"
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
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

func insertUser(t *testing.T, tx pgx.Tx) uuid.UUID {
	t.Helper()
	id := uuid.New()
	_, err := tx.Exec(context.Background(),
		`INSERT INTO auth_users (id, email, email_normalized, display_name, image_url, created_at, updated_at) VALUES ($1, $2, $2, '', '', now(), now())`,
		id, id.String()+"@example.org")
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestCreatePersonIsIdempotentPerUser(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	l := local(t)
	user := insertUser(t, tx)

	a, err := l.CreatePerson(ctx, tx, user, "ada", "Ada")
	if err != nil {
		t.Fatal(err)
	}
	if a.ApID != "https://notid.test/u/ada" || a.Inbox != a.ApID+"/inbox" || *a.Trust != "new" || !a.Local {
		t.Fatalf("unexpected actor: %+v", a)
	}
	again, err := l.CreatePerson(ctx, tx, user, "other", "")
	if err != nil || again.ID != a.ID {
		t.Fatalf("second create: %+v %v", again, err)
	}
}

func TestInvariant1PersonAPIDIsStoredAndNeverDerivedFromInternalKey(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	a, err := local(t).CreatePerson(ctx, tx, insertUser(t, tx), "grace", "")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains([]byte(a.ApID), []byte(uuidStr(a.UserID))) {
		t.Fatalf("ap_id %q contains the user id", a.ApID)
	}
	var stored string
	if err := tx.QueryRow(ctx, `SELECT ap_id FROM actors WHERE id = $1`, a.ID).Scan(&stored); err != nil || stored != a.ApID {
		t.Fatalf("stored ap_id %q %v", stored, err)
	}
	if _, err := tx.Exec(ctx, `UPDATE actors SET name = 'renamed' WHERE id = $1`, a.ID); err != nil {
		t.Fatal(err)
	}
	if err := tx.QueryRow(ctx, `SELECT ap_id FROM actors WHERE id = $1`, a.ID).Scan(&stored); err != nil || stored != a.ApID {
		t.Fatalf("ap_id changed with the name: %q", stored)
	}
}

func TestCreatePersonRejectsTakenAndInvalidNames(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	l := local(t)
	if _, err := l.CreatePerson(ctx, tx, insertUser(t, tx), "linus", ""); err != nil {
		t.Fatal(err)
	}
	if _, err := l.CreatePerson(ctx, tx, insertUser(t, tx), "linus", ""); !errors.Is(err, actors.ErrNameTaken) {
		t.Fatalf("taken name: %v", err)
	}
	for _, bad := range []string{"Linus", "ab", "1abc", "a-b", "abcdefghijklmnopqrstu"} {
		if _, err := l.CreatePerson(ctx, tx, insertUser(t, tx), bad, ""); !errors.Is(err, actors.ErrNameInvalid) {
			t.Errorf("%q: %v", bad, err)
		}
	}
}

func TestPrivateKeyIsSealedAtRest(t *testing.T) {
	ctx := context.Background()
	tx := pgtest.Tx(t)
	l := local(t)
	a, err := l.CreatePerson(ctx, tx, insertUser(t, tx), "keyed", "")
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(a.PrivateKeyEnc, []byte("PRIVATE KEY")) {
		t.Fatal("private key stored in plaintext")
	}
	pem, err := l.Sealer.Open(a.PrivateKeyEnc, actors.KeyPurpose)
	if err != nil || !bytes.Contains(pem, []byte("BEGIN PRIVATE KEY")) {
		t.Fatalf("cannot open sealed key: %v", err)
	}
}

func uuidStr(u *uuid.UUID) string {
	if u == nil {
		return "\x00"
	}
	return u.String()
}
