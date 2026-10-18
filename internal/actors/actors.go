// Package actors owns the actors table: local and remote persons and groups, their keys, and remote fetches.
package actors

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/alternayte/notid/internal/actors/actorsdb"
	"github.com/alternayte/notid/internal/platform"
)

type Actor = actorsdb.Actor

// KeyPurpose binds sealed actor private keys to their use.
const KeyPurpose = "actor-private-key"

var (
	ErrNameTaken   = errors.New("the name is taken")
	ErrNameInvalid = errors.New("a name has 3 to 20 characters: lowercase letters, digits and underscores, starting with a letter")
	ErrNotFound    = errors.New("actor not found")
)

var nameRE = regexp.MustCompile(`^[a-z][a-z0-9_]{2,19}$`)

func ValidName(name string) error {
	if !nameRE.MatchString(name) {
		return ErrNameInvalid
	}
	return nil
}

// Local creates and reads actors that live on this installation.
type Local struct {
	AppOrigin string
	AppDomain string
	Sealer    *platform.Sealer
}

// PersonURL is the AP ID of a local person. It is fixed at creation and stored, never recomputed.
func (l Local) PersonURL(name string) string { return l.AppOrigin + "/u/" + name }

// CreatePerson inserts the person actor of a user. It returns the existing actor when the user already has one.
func (l Local) CreatePerson(ctx context.Context, db actorsdb.DBTX, userID uuid.UUID, name, displayName string) (Actor, error) {
	if err := ValidName(name); err != nil {
		return Actor{}, err
	}
	q := actorsdb.New(db)
	if existing, err := q.ActorByUserID(ctx, &userID); err == nil {
		return existing, nil
	} else if !errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, fmt.Errorf("look up actor: %w", err)
	}
	taken, err := q.LocalNameTaken(ctx, actorsdb.LocalNameTakenParams{Domain: l.AppDomain, Name: name})
	if err != nil {
		return Actor{}, fmt.Errorf("check name: %w", err)
	}
	if taken {
		return Actor{}, ErrNameTaken
	}
	pub, priv, err := NewKeyPair()
	if err != nil {
		return Actor{}, err
	}
	apID := l.PersonURL(name)
	a, err := q.InsertLocalPerson(ctx, actorsdb.InsertLocalPersonParams{
		ApID:          apID,
		UserID:        &userID,
		Domain:        l.AppDomain,
		Name:          name,
		DisplayName:   displayName,
		Inbox:         apID + "/inbox",
		SharedInbox:   l.AppOrigin + "/inbox",
		PublicKeyPem:  pub,
		PrivateKeyEnc: l.Sealer.Seal(priv, KeyPurpose),
	})
	if errors.Is(err, pgx.ErrNoRows) {
		// A concurrent request created the actor first.
		return q.ActorByUserID(ctx, &userID)
	}
	if err != nil {
		return Actor{}, fmt.Errorf("insert actor: %w", err)
	}
	return a, nil
}

func ByUserID(ctx context.Context, db actorsdb.DBTX, userID uuid.UUID) (Actor, error) {
	a, err := actorsdb.New(db).ActorByUserID(ctx, &userID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Actor{}, ErrNotFound
	}
	return a, err
}

// NewKeyPair returns an RSA 2048 key, the size Lemmy accepts, as a public PEM and a private PKCS#8 PEM.
func NewKeyPair() (string, []byte, error) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return "", nil, fmt.Errorf("generate key: %w", err)
	}
	pubDER, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		return "", nil, fmt.Errorf("marshal public key: %w", err)
	}
	privDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		return "", nil, fmt.Errorf("marshal private key: %w", err)
	}
	pub := pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: pubDER})
	priv := pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privDER})
	return string(pub), priv, nil
}

// GroupURL is the AP ID of a community's group on its domain. It is fixed at creation and stored.
func (l Local) GroupURL(domain, name string) string {
	return scheme(l.AppOrigin) + "://" + l.host(domain) + "/c/" + name
}

// host keeps the app origin's port, which dev and the harness need. One
// listener serves the app domain and every community domain.
func (l Local) host(domain string) string {
	appHost := strings.TrimPrefix(strings.TrimPrefix(l.AppOrigin, "https://"), "http://")
	if domain == l.AppDomain {
		return appHost
	}
	if _, port, err := net.SplitHostPort(appHost); err == nil {
		return net.JoinHostPort(domain, port)
	}
	return domain
}

func scheme(origin string) string {
	if strings.HasPrefix(origin, "http://") {
		return "http"
	}
	return "https"
}

// CreateGroup inserts the group actor of a new community with its own key.
func (l Local) CreateGroup(ctx context.Context, db actorsdb.DBTX, domain, name, title, summary string) (Actor, error) {
	if err := ValidName(name); err != nil {
		return Actor{}, err
	}
	pub, priv, err := NewKeyPair()
	if err != nil {
		return Actor{}, err
	}
	apID := l.GroupURL(domain, name)
	a, err := actorsdb.New(db).InsertLocalGroup(ctx, actorsdb.InsertLocalGroupParams{
		ApID: apID, Domain: domain, Name: name, DisplayName: title, Summary: summary,
		Inbox: apID + "/inbox", SharedInbox: apID + "/inbox",
		PublicKeyPem: pub, PrivateKeyEnc: l.Sealer.Seal(priv, KeyPurpose),
	})
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return Actor{}, ErrNameTaken
	}
	if err != nil {
		return Actor{}, fmt.Errorf("insert group actor: %w", err)
	}
	return a, nil
}
