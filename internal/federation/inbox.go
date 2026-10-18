package federation

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/federation/federationdb"
	"github.com/alternayte/notid/internal/moderation"
	"github.com/alternayte/notid/internal/platform"
)

// rejection is an activity the inbox refuses, with the status it answers.
type rejection struct {
	status int
	msg    string
}

func (r rejection) Error() string { return r.msg }

func reject(status int, format string, args ...any) error {
	return rejection{status: status, msg: fmt.Sprintf(format, args...)}
}

// InboxArgs is a verified activity waiting to be applied.
type InboxArgs struct {
	Activity json.RawMessage `json:"activity"`
	Signer   string          `json:"signer"`
}

func (InboxArgs) Kind() string { return "inbox" }

func (InboxArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueInbox, MaxAttempts: 10}
}

const (
	queueInbox   = "inbox"
	queueDeliver = "deliver"
)

// inbox accepts an activity: it verifies the signature, refuses blocked and
// over-limit peers, ignores duplicates and checks hosts, then queues the
// activity in the same transaction that records its ID. It answers 2xx only
// for an activity it queued now or queued before.
func (s *Server) inbox(w http.ResponseWriter, r *http.Request) {
	act, err := s.receive(r)
	var rej rejection
	switch {
	case err == nil:
		w.WriteHeader(http.StatusAccepted)
	case errors.As(err, &rej):
		s.Log.InfoContext(r.Context(), "inbox refused", "path", r.URL.Path, "type", act.Type, "id", act.ID, "status", rej.status, "reason", rej.msg)
		http.Error(w, rej.msg, rej.status)
	default:
		s.Log.ErrorContext(r.Context(), "inbox", "path", r.URL.Path, "err", err)
		http.Error(w, "the activity could not be stored; retry later", http.StatusInternalServerError)
	}
}

// receive returns the activity as far as it was read, for the log.
func (s *Server) receive(r *http.Request) (activity, error) {
	ctx := r.Context()
	var act activity
	body, err := io.ReadAll(http.MaxBytesReader(nil, r.Body, s.Tuning.MaxInboxBytes))
	if err != nil {
		return act, reject(http.StatusRequestEntityTooLarge, "body too large or unreadable")
	}
	sig, err := parseSignature(r)
	if err != nil {
		return act, reject(http.StatusUnauthorized, "%v", err)
	}
	peer := hostOf(keyOwner(sig.KeyID))
	if peer == "" {
		return act, reject(http.StatusUnauthorized, "keyId is not an http(s) URL")
	}
	if err := s.checkBlocked(ctx, peer); err != nil {
		return act, err
	}
	if err := checkSignedRequest(r, sig, body, time.Now(), s.Tuning.SignatureMaxSkew); err != nil {
		return act, reject(http.StatusUnauthorized, "%v", err)
	}
	signer, err := s.verifySigner(ctx, r, sig)
	if err != nil {
		return act, err
	}
	// Charged only once the signature holds, so a forged keyId cannot spend a real peer's allowance.
	if !s.Limits.Allow(peer, time.Now()) {
		return act, reject(http.StatusTooManyRequests, "too many activities from %s", peer)
	}
	// A site ban stops the person here, not only in the web app.
	if banned, err := moderation.SiteBanned(ctx, s.Pool, signer.ID); err != nil {
		return act, err
	} else if banned {
		return act, reject(http.StatusForbidden, "this site banned the sender")
	}

	if err := json.Unmarshal(body, &act); err != nil {
		return act, reject(http.StatusBadRequest, "body is not an activity: %v", err)
	}
	if act.ID == "" || act.Type == "" || act.Actor.ID == "" {
		return act, reject(http.StatusBadRequest, "activity lacks id, type or actor")
	}
	if act.Actor.ID != signer.ApID {
		return act, reject(http.StatusUnauthorized, "activity actor %s is not the signer %s", act.Actor.ID, signer.ApID)
	}
	if err := checkHosts(body); err != nil {
		return act, reject(http.StatusBadRequest, "%v", err)
	}
	if err := s.validate(ctx, act); err != nil {
		return act, err
	}
	return act, s.enqueueReceived(ctx, act.ID, body, signer.ApID, peer)
}

func (s *Server) checkBlocked(ctx context.Context, peer string) error {
	p, err := federationdb.New(s.Pool).PeerByDomain(ctx, peer)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("peer %s: %w", peer, err)
	}
	if err == nil && p.Blocked {
		return reject(http.StatusForbidden, "this installation does not federate with %s", peer)
	}
	return nil
}

// keyStored reports whether checking a key needs no fetch.
func (s *Server) keyStored(ctx context.Context, keyID string) bool {
	a, err := actors.ByApID(ctx, s.Pool, keyOwner(keyID))
	return err == nil && (a.Local || (a.FetchedAt != nil && time.Since(*a.FetchedAt) < s.Tuning.ActorRefresh))
}

// verifySigner checks the signature against the signer's key. When it fails,
// it fetches the actor once more, because the key may have rotated.
func (s *Server) verifySigner(ctx context.Context, r *http.Request, sig signatureParams) (actors.Actor, error) {
	if !s.keyStored(ctx, sig.KeyID) && !s.KeyFetches.Allow(platform.ClientIP(ctx), time.Now()) {
		return actors.Actor{}, reject(http.StatusTooManyRequests, "too many unknown keys from this address")
	}
	signer, err := s.actorForKey(ctx, sig.KeyID, false)
	if err != nil {
		return actors.Actor{}, reject(http.StatusUnauthorized, "signer unknown: %v", err)
	}
	if verifyWithKey(r, sig, signer.PublicKeyPem) == nil {
		return signer, nil
	}
	if signer.Local {
		return actors.Actor{}, reject(http.StatusUnauthorized, "%v", errBadSignature)
	}
	if !s.KeyFetches.Allow(platform.ClientIP(ctx), time.Now()) {
		return actors.Actor{}, reject(http.StatusTooManyRequests, "too many failed signatures from this address")
	}
	signer, err = s.actorForKey(ctx, sig.KeyID, true)
	if err != nil {
		return actors.Actor{}, reject(http.StatusUnauthorized, "signer refetch: %v", err)
	}
	if err := verifyWithKey(r, sig, signer.PublicKeyPem); err != nil {
		return actors.Actor{}, reject(http.StatusUnauthorized, "%v", err)
	}
	return signer, nil
}

// enqueueReceived records the activity ID and queues the activity in one
// transaction, so a duplicate ID is queued at most once.
func (s *Server) enqueueReceived(ctx context.Context, id string, body []byte, signer, peer string) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := federationdb.New(tx)
	if _, err := q.RecordActivity(ctx, id); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("record activity: %w", err)
	}
	if _, err := s.River.InsertTx(ctx, tx, InboxArgs{Activity: body, Signer: signer}, nil); err != nil {
		return fmt.Errorf("queue activity: %w", err)
	}
	if err := q.PeerOK(ctx, peer); err != nil {
		return fmt.Errorf("mark peer: %w", err)
	}
	return tx.Commit(ctx)
}

// inboxWorker applies a queued activity.
type inboxWorker struct {
	river.WorkerDefaults[InboxArgs]
	s *Server
}

func (w *inboxWorker) Work(ctx context.Context, job *river.Job[InboxArgs]) error {
	var act activity
	if err := json.Unmarshal(job.Args.Activity, &act); err != nil {
		return river.JobCancel(fmt.Errorf("decode activity: %w", err))
	}
	tx, err := w.s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := w.s.apply(ctx, tx, act, job.Args.Activity); err != nil {
		return fmt.Errorf("apply %s %s: %w", act.Type, act.ID, err)
	}
	return tx.Commit(ctx)
}
