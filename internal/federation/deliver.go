package federation

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/federation/federationdb"
)

// DeliverArgs is one activity for one inbox, signed by a local actor at send time.
type DeliverArgs struct {
	Inbox    string          `json:"inbox"`
	Activity json.RawMessage `json:"activity"`
	SignerID int64           `json:"signer_id"`
	// PostID or CommentID names the local content this activity writes, so the
	// job can record the peer's answer on it.
	PostID    int64 `json:"post_id,omitempty"`
	CommentID int64 `json:"comment_id,omitempty"`
}

func (DeliverArgs) Kind() string { return "deliver" }

// The retry window, not the attempt count, ends a delivery; this cap only
// bounds a misconfigured window.
func (DeliverArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueDeliver, MaxAttempts: 100}
}

// deliver queues activity for inbox inside tx, so the write and its delivery
// commit together.
func (s *Server) deliver(ctx context.Context, tx pgx.Tx, signerID int64, inbox string, activity any) error {
	return s.deliverContent(ctx, tx, signerID, inbox, activity, 0, 0)
}

// deliverContent queues an activity about a local post or comment and records
// on that row that the peer has not answered yet.
func (s *Server) deliverContent(ctx context.Context, tx pgx.Tx, signerID int64, inbox string, activity any, postID, commentID int64) error {
	body, err := json.Marshal(activity)
	if err != nil {
		return fmt.Errorf("encode activity: %w", err)
	}
	if postID != 0 || commentID != 0 {
		if err := discussions.RecordDelivery(ctx, tx, postID, commentID, "pending", ""); err != nil {
			return err
		}
	}
	args := DeliverArgs{Inbox: inbox, Activity: body, SignerID: signerID, PostID: postID, CommentID: commentID}
	if _, err := s.River.InsertTx(ctx, tx, args, nil); err != nil {
		return fmt.Errorf("queue delivery to %s: %w", inbox, err)
	}
	return nil
}

// refusal is a peer's 4xx answer. A retry cannot change it, apart from a 400,
// which some peers also answer for their own transient failures.
type refusal struct {
	status int
	msg    string
	inbox  string
}

func (r *refusal) Error() string {
	return fmt.Sprintf("the peer refused the activity: %s answered %d: %s", r.inbox, r.status, r.msg)
}

// reason is the peer's answer without the inbox, for the author to read.
func (r *refusal) reason() string {
	if r.msg == "" {
		return strconv.Itoa(r.status)
	}
	return fmt.Sprintf("%d: %s", r.status, r.msg)
}

type deliverWorker struct {
	river.WorkerDefaults[DeliverArgs]
	s *Server
}

// NextRetry doubles the wait from 30 s up to the tuning cap.
func (w *deliverWorker) NextRetry(job *river.Job[DeliverArgs]) time.Time {
	d := 30 * time.Second
	for i := 1; i < job.Attempt && d < w.s.Tuning.DeliveryMaxBackoff; i++ {
		d *= 2
	}
	return time.Now().Add(min(d, w.s.Tuning.DeliveryMaxBackoff))
}

func (w *deliverWorker) Work(ctx context.Context, job *river.Job[DeliverArgs]) error {
	s := w.s
	peer := hostOf(job.Args.Inbox)
	q := federationdb.New(s.Pool)
	p, err := q.PeerByDomain(ctx, peer)
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return fmt.Errorf("peer %s: %w", peer, err)
	}
	if err == nil {
		if p.Blocked {
			w.rejected(ctx, job.Args, "this installation does not federate with "+peer)
			return river.JobCancel(fmt.Errorf("peer %s is blocked", peer))
		}
		if p.FailingSince != nil && time.Since(*p.FailingSince) > s.Tuning.DeliveryRetryWindow {
			w.rejected(ctx, job.Args, "no answer from "+peer+" for "+s.Tuning.DeliveryRetryWindow.String())
			return river.JobCancel(fmt.Errorf("peer %s has failed since %s", peer, p.FailingSince.Format(time.RFC3339)))
		}
	}

	err = s.post(ctx, job.Args)
	var local *river.JobCancelError
	var ref *refusal
	switch {
	case errors.As(err, &local):
		return err
	case err == nil:
		w.record(ctx, job.Args, "delivered", "")
		return q.PeerOK(ctx, peer)
	case errors.As(err, &ref) && ref.status == http.StatusBadRequest && job.Attempt < w.s.Tuning.DeliveryBadRequestRetries:
		// Lemmy answers 400 when its own fetch of an actor fails, for example
		// over stale DNS during a move; a few retries let that pass.
		return err
	case errors.As(err, &ref):
		// The peer answered, so it is reachable.
		if okErr := q.PeerOK(ctx, peer); okErr != nil {
			s.Log.ErrorContext(ctx, "mark peer", "peer", peer, "err", okErr)
		}
		w.rejected(ctx, job.Args, ref.reason())
		return river.JobCancel(err)
	}
	if failErr := q.PeerFailing(ctx, peer); failErr != nil {
		s.Log.ErrorContext(ctx, "mark peer", "peer", peer, "err", failErr)
	}
	if time.Since(job.CreatedAt) > s.Tuning.DeliveryRetryWindow {
		w.rejected(ctx, job.Args, "no answer for "+s.Tuning.DeliveryRetryWindow.String())
		return river.JobCancel(fmt.Errorf("gave up after %s: %w", s.Tuning.DeliveryRetryWindow, err))
	}
	return err
}

// rejected records that a peer will not have the activity.
func (w *deliverWorker) rejected(ctx context.Context, args DeliverArgs, reason string) {
	w.record(ctx, args, "rejected", reason)
}

func (w *deliverWorker) record(ctx context.Context, args DeliverArgs, state, reason string) {
	if args.PostID == 0 && args.CommentID == 0 {
		return
	}
	if err := discussions.RecordDelivery(ctx, w.s.Pool, args.PostID, args.CommentID, state, reason); err != nil {
		w.s.Log.ErrorContext(ctx, "record delivery", "post", args.PostID, "comment", args.CommentID, "err", err)
	}
}

// post signs and sends one delivery. It wraps errPermanent for a 4xx answer
// other than 408 and 429.
func (s *Server) post(ctx context.Context, args DeliverArgs) error {
	signer, err := actors.ByID(ctx, s.Pool, args.SignerID)
	if err != nil {
		return river.JobCancel(fmt.Errorf("signer %d: %w", args.SignerID, err))
	}
	key, err := s.Local.PrivateKey(signer)
	if err != nil {
		// A departed community has deleted its key; it must not send as the group.
		return river.JobCancel(err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, args.Inbox, bytes.NewReader(args.Activity))
	if err != nil {
		return river.JobCancel(fmt.Errorf("inbox %s: %w", args.Inbox, err))
	}
	req.Header.Set("Content-Type", "application/activity+json")
	req.Header.Set("User-Agent", s.userAgent())
	if err := signRequest(req, signer.ApID+"#main-key", key, args.Activity, time.Now()); err != nil {
		return err
	}
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("post to %s: %w", args.Inbox, err)
	}
	defer resp.Body.Close()
	msg, _ := io.ReadAll(io.LimitReader(resp.Body, 512))
	switch {
	case resp.StatusCode >= 200 && resp.StatusCode < 300:
		return nil
	case resp.StatusCode >= 400 && resp.StatusCode < 500 &&
		resp.StatusCode != http.StatusRequestTimeout && resp.StatusCode != http.StatusTooManyRequests:
		return &refusal{status: resp.StatusCode, msg: string(bytes.TrimSpace(msg)), inbox: args.Inbox}
	default:
		return fmt.Errorf("%s answered %d: %s", args.Inbox, resp.StatusCode, bytes.TrimSpace(msg))
	}
}

// AddWorkers registers the inbox, delivery, follow-check and backfill workers.
func (s *Server) AddWorkers(workers *river.Workers) {
	river.AddWorker(workers, &inboxWorker{s: s})
	river.AddWorker(workers, &deliverWorker{s: s})
	river.AddWorker(workers, &followCheckWorker{s: s})
	river.AddWorker(workers, &backfillWorker{s: s})
	river.AddWorker(workers, &indexWorker{s: s})
	river.AddWorker(workers, &indexAllWorker{s: s})
}

// Queues is the River queue config for the federation workers.
func (s *Server) Queues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{
		queueInbox:   {MaxWorkers: s.Tuning.InboxWorkers},
		queueDeliver: {MaxWorkers: s.Tuning.DeliveryWorkers},
	}
}
