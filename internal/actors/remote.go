package actors

import (
	"context"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors/actorsdb"
)

// ErrNotRemote means an upsert targeted a local actor or changed an actor's kind.
var ErrNotRemote = errors.New("the AP ID belongs to a local actor or to an actor of another kind")

// Remote is what notid keeps of an actor that lives on a peer.
type Remote struct {
	ApID         string
	Kind         string
	Domain       string
	Name         string
	DisplayName  string
	Summary      string
	Inbox        string
	SharedInbox  string
	PublicKeyPem string
	Bot          bool
}

// UpsertRemote stores a freshly fetched remote actor. It never touches a local actor.
func UpsertRemote(ctx context.Context, db actorsdb.DBTX, r Remote) (Actor, error) {
	a, err := actorsdb.New(db).UpsertRemoteActor(ctx, actorsdb.UpsertRemoteActorParams{
		ApID: r.ApID, Kind: r.Kind, Domain: r.Domain, Name: r.Name, DisplayName: r.DisplayName,
		Summary: r.Summary, Inbox: r.Inbox, SharedInbox: r.SharedInbox, PublicKeyPem: r.PublicKeyPem, Bot: r.Bot,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, fmt.Errorf("%s: %w", r.ApID, ErrNotRemote)
	}
	if err != nil {
		return Actor{}, fmt.Errorf("upsert remote actor %s: %w", r.ApID, err)
	}
	return a, nil
}

func ByApID(ctx context.Context, db actorsdb.DBTX, apID string) (Actor, error) {
	a, err := actorsdb.New(db).ActorByApID(ctx, apID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrNotFound
	}
	return a, err
}

// MarkDeleted tombstones the actor of a remote person who deleted their
// account. The row stays so their content and reports keep a name.
func MarkDeleted(ctx context.Context, db actorsdb.DBTX, id int64) error {
	if err := actorsdb.New(db).MarkRemoteActorDeleted(ctx, id); err != nil {
		return fmt.Errorf("mark actor %d deleted: %w", id, err)
	}
	return nil
}

func ByID(ctx context.Context, db actorsdb.DBTX, id int64) (Actor, error) {
	a, err := actorsdb.New(db).ActorByID(ctx, id)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrNotFound
	}
	return a, err
}

// ApplicationURL is the AP ID of the app domain's Application actor, which signs fetches.
func (l Local) ApplicationURL() string { return l.AppOrigin + "/actor" }

// Application returns the app domain's Application actor and creates it on first use.
func (l Local) Application(ctx context.Context, db actorsdb.DBTX) (Actor, error) {
	apID := l.ApplicationURL()
	if a, err := ByApID(ctx, db, apID); !errors.Is(err, ErrNotFound) {
		return a, err
	}
	pub, priv, err := NewKeyPair()
	if err != nil {
		return Actor{}, err
	}
	a, err := actorsdb.New(db).InsertApplication(ctx, actorsdb.InsertApplicationParams{
		ApID: apID, Domain: l.AppDomain, Name: l.AppDomain,
		Inbox: l.AppOrigin + "/inbox", SharedInbox: l.AppOrigin + "/inbox",
		PublicKeyPem: pub, PrivateKeyEnc: l.Sealer.Seal(priv, KeyPurpose),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return ByApID(ctx, db, apID)
	}
	if err != nil {
		return Actor{}, fmt.Errorf("insert application actor: %w", err)
	}
	return a, nil
}

// PrivateKey opens a local actor's sealed signing key.
func (l Local) PrivateKey(a Actor) (*rsa.PrivateKey, error) {
	if !a.Local || a.PrivateKeyEnc == nil {
		return nil, fmt.Errorf("actor %s has no private key here", a.ApID)
	}
	raw, err := l.Sealer.Open(a.PrivateKeyEnc, KeyPurpose)
	if err != nil {
		return nil, err
	}
	block, _ := pem.Decode(raw)
	if block == nil {
		return nil, fmt.Errorf("actor %s: private key is not PEM", a.ApID)
	}
	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("actor %s: parse private key: %w", a.ApID, err)
	}
	rk, ok := key.(*rsa.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("actor %s: private key is not RSA", a.ApID)
	}
	return rk, nil
}

// GroupByHandle finds a group this installation already knows by its handle.
func GroupByHandle(ctx context.Context, db actorsdb.DBTX, domain, name string) (Actor, error) {
	a, err := actorsdb.New(db).GroupByHandle(ctx, actorsdb.GroupByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrNotFound
	}
	return a, err
}
