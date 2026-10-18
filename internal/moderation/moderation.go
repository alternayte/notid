// Package moderation owns removals, locks, bans, reports and the public
// moderation log. Every action writes its log row in its own transaction.
package moderation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/moderation/moderationdb"
	"github.com/alternayte/notid/internal/platform"
)

type Service struct {
	Pool   *pgxpool.Pool
	Local  actors.Local
	Tuning platform.Tuning
}

var (
	ErrNotModerator = errors.New("only a community's owners and moderators can do that")
	ErrNoObject     = errors.New("no post, comment or report at that address")
	ErrAlreadyDone  = errors.New("that action was already taken")
	ErrBanned       = errors.New("you are banned from this community")
	ErrSelfBan      = errors.New("you cannot ban yourself")
)

// Actions recorded in the moderation log.
const (
	ActionRemovePost      = "remove_post"
	ActionRestorePost     = "restore_post"
	ActionRemoveComment   = "remove_comment"
	ActionRestoreComment  = "restore_comment"
	ActionLock            = "lock"
	ActionUnlock          = "unlock"
	ActionBan             = "ban"
	ActionUnban           = "unban"
	ActionReportUpheld    = "report_upheld"
	ActionReportDismissed = "report_dismissed"
)

// Target is what an action changed.
type Target struct {
	ApID      string
	Ref       string
	Community int64
	AuthorID  int64
	PostApID  string
}

func (s *Service) inTx(ctx context.Context, fn func(tx pgx.Tx) error) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	if err := fn(tx); err != nil {
		return err
	}
	if err := tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit: %w", err)
	}
	return nil
}

// moderator checks that actor holds a moderator or owner role in community.
func moderator(ctx context.Context, q *moderationdb.Queries, communityID, actorID int64) error {
	role, err := q.ModeratorOf(ctx, moderationdb.ModeratorOfParams{CommunityID: communityID, ActorID: actorID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && role == "member") {
		return ErrNotModerator
	}
	if err != nil {
		return fmt.Errorf("membership: %w", err)
	}
	return nil
}

// CanModerate reports whether actor holds a moderator or owner role in community.
func CanModerate(ctx context.Context, db moderationdb.DBTX, communityID, actorID int64) (bool, error) {
	return moderator(ctx, moderationdb.New(db), communityID, actorID) == nil, nil
}

// Banned reports whether actor is banned from community now. A ban whose
// expiry has passed no longer applies.
func Banned(ctx context.Context, db moderationdb.DBTX, communityID, actorID int64) (bool, error) {
	_, err := moderationdb.New(db).ActiveBan(ctx, moderationdb.ActiveBanParams{CommunityID: &communityID, ActorID: actorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ban: %w", err)
	}
	return true, nil
}

// writeLog records one action. The caller has already checked the role.
func writeLog(ctx context.Context, q *moderationdb.Queries, communityID, moderatorID int64, action string, targetActor *int64, targetApID, reason string, expires *time.Time) error {
	params := moderationdb.InsertModerationLogParams{
		CommunityID: &communityID, ModeratorID: moderatorID, Action: action,
		TargetActorID: targetActor, Reason: reason, ExpiresAt: expires,
	}
	if targetApID != "" {
		params.TargetApID = &targetApID
	}
	if _, err := q.InsertModerationLog(ctx, params); err != nil {
		return fmt.Errorf("moderation log: %w", err)
	}
	return nil
}

// locate finds the post or comment, and whether the object is a comment.
func locate(ctx context.Context, q *moderationdb.Queries, objectApID string) (Target, bool, error) {
	p, err := q.PostForModeration(ctx, objectApID)
	if err == nil {
		return Target{ApID: p.ApID, Ref: p.Ref, Community: p.CommunityID, AuthorID: p.AuthorID}, false, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return Target{}, false, fmt.Errorf("post: %w", err)
	}
	c, err := q.CommentForModeration(ctx, objectApID)
	if errors.Is(err, pgx.ErrNoRows) {
		return Target{}, false, ErrNoObject
	}
	if err != nil {
		return Target{}, false, fmt.Errorf("comment: %w", err)
	}
	return Target{ApID: c.ApID, Ref: c.Ref, Community: c.CommunityID, AuthorID: c.AuthorID, PostApID: c.PostApID}, true, nil
}

// Remove hides a post or a comment for everyone and records why.
func (s *Service) Remove(ctx context.Context, actor actors.Actor, objectApID, reason string) (Target, error) {
	var t Target
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		t, isComment, err := locate(ctx, q, objectApID)
		if err != nil {
			return err
		}
		if err := moderator(ctx, q, t.Community, actor.ID); err != nil {
			return err
		}
		action := ActionRemovePost
		if isComment {
			_, err = q.RemoveComment(ctx, objectApID)
			action = ActionRemoveComment
		} else {
			_, err = q.RemovePost(ctx, objectApID)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyDone
		}
		if err != nil {
			return fmt.Errorf("remove: %w", err)
		}
		return writeLog(ctx, q, t.Community, actor.ID, action, &t.AuthorID, objectApID, reason, nil)
	})
	return t, err
}

// Restore undoes a removal.
func (s *Service) Restore(ctx context.Context, actor actors.Actor, objectApID string) (Target, error) {
	var t Target
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		t, isComment, err := locate(ctx, q, objectApID)
		if err != nil {
			return err
		}
		if err := moderator(ctx, q, t.Community, actor.ID); err != nil {
			return err
		}
		action := ActionRestorePost
		if isComment {
			_, err = q.RestoreComment(ctx, objectApID)
			action = ActionRestoreComment
		} else {
			_, err = q.RestorePost(ctx, objectApID)
		}
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyDone
		}
		if err != nil {
			return fmt.Errorf("restore: %w", err)
		}
		return writeLog(ctx, q, t.Community, actor.ID, action, &t.AuthorID, objectApID, "", nil)
	})
	return t, err
}

// SetLocked locks or unlocks replies to a post.
func (s *Service) SetLocked(ctx context.Context, actor actors.Actor, postApID string, locked bool) (Target, error) {
	var t Target
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		t, _, err := locate(ctx, q, postApID)
		if err != nil {
			return err
		}
		if err := moderator(ctx, q, t.Community, actor.ID); err != nil {
			return err
		}
		if _, err := q.SetPostLocked(ctx, moderationdb.SetPostLockedParams{ApID: postApID, Locked: locked}); err != nil {
			return fmt.Errorf("lock post: %w", err)
		}
		action := ActionUnlock
		if locked {
			action = ActionLock
		}
		return writeLog(ctx, q, t.Community, actor.ID, action, &t.AuthorID, postApID, "", nil)
	})
	return t, err
}

// Ban stops an actor posting in a community until expires (nil: until unbanned)
// and removes them from its members.
func (s *Service) Ban(ctx context.Context, actor actors.Actor, communityID, targetID int64, reason string, expires *time.Time) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		if err := moderator(ctx, q, communityID, actor.ID); err != nil {
			return err
		}
		if targetID == actor.ID {
			return ErrSelfBan
		}
		if _, err := q.InsertBan(ctx, moderationdb.InsertBanParams{CommunityID: &communityID, ActorID: targetID, ExpiresAt: expires}); err != nil {
			return fmt.Errorf("ban: %w", err)
		}
		if err := q.DeleteMembershipForBan(ctx, moderationdb.DeleteMembershipForBanParams{CommunityID: communityID, ActorID: targetID}); err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		return writeLog(ctx, q, communityID, actor.ID, ActionBan, &targetID, "", reason, expires)
	})
}

// Unban lifts a ban.
func (s *Service) Unban(ctx context.Context, actor actors.Actor, communityID, targetID int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		if err := moderator(ctx, q, communityID, actor.ID); err != nil {
			return err
		}
		if err := q.DeleteBan(ctx, moderationdb.DeleteBanParams{CommunityID: &communityID, ActorID: targetID}); err != nil {
			return fmt.Errorf("unban: %w", err)
		}
		return writeLog(ctx, q, communityID, actor.ID, ActionUnban, &targetID, "", "", nil)
	})
}

// Report is one report as moderators see it, with who reported it.
type Report struct {
	ID          int64     `json:"id"`
	Reason      string    `json:"reason"`
	State       string    `json:"state" enum:"open,upheld,dismissed"`
	CreatedAt   time.Time `json:"createdAt"`
	Reporter    string    `json:"reporter" doc:"Who reported it; shown to moderators only"`
	PostRef     string    `json:"postRef,omitempty"`
	PostApID    string    `json:"postApId,omitempty"`
	PostTitle   string    `json:"postTitle,omitempty"`
	CommentRef  string    `json:"commentRef,omitempty"`
	CommentBody string    `json:"commentBody,omitempty" doc:"Plain text excerpt"`
}

// File records a report on a post or a comment. Any signed-in person can
// report; reporters never appear in public.
func (s *Service) File(ctx context.Context, reporter actors.Actor, objectApID, reason string) (int64, error) {
	var id int64
	err := s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		params := moderationdb.InsertReportParams{ReporterID: reporter.ID, Reason: reason}
		if p, err := q.PostForModeration(ctx, objectApID); err == nil {
			params.CommunityID, params.PostID = p.CommunityID, &p.ID
		} else if !errors.Is(err, pgx.ErrNoRows) {
			return fmt.Errorf("post: %w", err)
		} else if c, err := q.CommentForModeration(ctx, objectApID); err == nil {
			params.CommunityID, params.CommentID = c.CommunityID, &c.ID
		} else if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		} else {
			return fmt.Errorf("comment: %w", err)
		}
		r, err := q.InsertReport(ctx, params)
		if err != nil {
			return fmt.Errorf("report: %w", err)
		}
		id = r.ID
		return nil
	})
	return id, err
}

// Queue lists the open reports of a community for its moderators.
func (s *Service) Queue(ctx context.Context, actor actors.Actor, handle string) ([]Report, error) {
	q := moderationdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(handle)
	communityID, err := q.CommunityIDByHandle(ctx, moderationdb.CommunityIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoObject
	}
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	if err := moderator(ctx, q, communityID, actor.ID); err != nil {
		return nil, err
	}
	rows, err := q.ReportQueue(ctx, moderationdb.ReportQueueParams{CommunityID: communityID, Lim: s.Tuning.ReportQueueSize})
	if err != nil {
		return nil, fmt.Errorf("report queue: %w", err)
	}
	out := make([]Report, 0, len(rows))
	for _, r := range rows {
		rep := Report{
			ID: r.ID, Reason: r.Reason, State: r.State, CreatedAt: r.CreatedAt,
			Reporter: s.Local.Handle(r.ReporterName, r.ReporterDomain),
		}
		if r.PostRef != nil {
			rep.PostRef, rep.PostApID, rep.PostTitle = *r.PostRef, *r.PostApID, *r.PostTitle
		}
		if r.CommentRef != nil {
			rep.CommentRef = *r.CommentRef
			if r.CommentBody != nil {
				rep.CommentBody = *r.CommentBody
			}
		}
		out = append(out, rep)
	}
	return out, nil
}

// Resolve closes a report as upheld or dismissed.
func (s *Service) Resolve(ctx context.Context, actor actors.Actor, reportID int64, upheld bool) error {
	state, action := "dismissed", ActionReportDismissed
	if upheld {
		state, action = "upheld", ActionReportUpheld
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		communityID, err := q.ReportCommunity(ctx, reportID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("report: %w", err)
		}
		if err := moderator(ctx, q, communityID, actor.ID); err != nil {
			return err
		}
		if _, err := q.ResolveReport(ctx, moderationdb.ResolveReportParams{ID: reportID, State: state, ResolvedBy: &actor.ID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyDone
		} else if err != nil {
			return fmt.Errorf("resolve report: %w", err)
		}
		return writeLog(ctx, q, communityID, actor.ID, action, nil, "", "", nil)
	})
}

// Entry is one line of the public moderation log.
type Entry struct {
	ID         int64      `json:"id"`
	Action     string     `json:"action"`
	Reason     string     `json:"reason"`
	CreatedAt  time.Time  `json:"createdAt"`
	ExpiresAt  *time.Time `json:"expiresAt,omitempty"`
	Moderator  string     `json:"moderator" doc:"Handle of the moderator or owner"`
	Target     string     `json:"target,omitempty" doc:"Handle of the person acted on"`
	TargetApID string     `json:"targetApId,omitempty"`
}

// Log lists a community's public moderation log, newest first.
func (s *Service) Log(ctx context.Context, handle string) ([]Entry, error) {
	q := moderationdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(handle)
	communityID, err := q.CommunityIDByHandle(ctx, moderationdb.CommunityIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, ErrNoObject
	}
	if err != nil {
		return nil, fmt.Errorf("community: %w", err)
	}
	rows, err := q.ModerationLog(ctx, moderationdb.ModerationLogParams{CommunityID: &communityID, Lim: s.Tuning.ModerationLogSize})
	if err != nil {
		return nil, fmt.Errorf("moderation log: %w", err)
	}
	out := make([]Entry, 0, len(rows))
	for _, r := range rows {
		e := Entry{
			ID: r.ID, Action: r.Action, Reason: r.Reason, CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
			Moderator: s.Local.Handle(r.ModeratorName, r.ModeratorDomain),
		}
		if r.TargetName != nil && r.TargetDomain != nil {
			e.Target = s.Local.Handle(*r.TargetName, *r.TargetDomain)
		}
		if r.TargetApID != nil {
			e.TargetApID = *r.TargetApID
		}
		out = append(out, e)
	}
	return out, nil
}
