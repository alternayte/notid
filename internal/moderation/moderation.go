// Package moderation owns removals, locks, bans, reports and the public
// moderation log. Every action writes its log row in its own transaction.
package moderation

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
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
	// IsAdmin reports whether the person holds auth-all's admin role.
	IsAdmin func(ctx context.Context, a actors.Actor) (bool, error)
	// SendLock federates a local community's lock or unlock of a post.
	SendLock func(ctx context.Context, tx pgx.Tx, moderator actors.Actor, communityID int64, postApID string, locked bool) error
	// SendBlock federates a community ban or its lifting of a remote person.
	SendBlock func(ctx context.Context, tx pgx.Tx, moderator actors.Actor, communityID int64, personApID string, blocked bool) error
	// SendRole federates an appointment or removal of a moderator.
	SendRole func(ctx context.Context, tx pgx.Tx, owner actors.Actor, communityID int64, personApID string, moderator bool) error
	// SendFlag federates a report on content in a remote community, as its
	// reporter, and returns the Flag's activity ID.
	SendFlag func(ctx context.Context, tx pgx.Tx, communityID int64, reporter actors.Actor, objectApID, reason string) (string, error)
	// QueueIndex queues a read of a server's community list into the community index.
	QueueIndex func(ctx context.Context, tx pgx.Tx, server string) error
	Logger     *slog.Logger
}

var (
	ErrNotModerator = errors.New("only a community's owners and moderators can do that")
	ErrNotAdmin     = errors.New("only a site admin can do that")
	ErrNoObject     = errors.New("no post, comment or report at that address")
	ErrAlreadyDone  = errors.New("that action was already taken")
	ErrBanned       = errors.New("you are banned from this community")
	ErrSelfBan      = errors.New("you cannot ban yourself")
	ErrReported     = errors.New("you already reported this")
	ErrReportLimit  = errors.New("you have filed too many reports today")
	ErrOutranked    = errors.New("an owner cannot be banned, and only an owner can ban a moderator")
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
	ActionAddModerator    = "add_moderator"
	ActionRemoveModerator = "remove_moderator"
	ActionSiteBan         = "site_ban"
	ActionSiteUnban       = "site_unban"
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
	// Local is true when a person on this installation wrote the target.
	Local bool
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
func writeLog(ctx context.Context, q *moderationdb.Queries, communityID *int64, moderatorID int64, action string, targetActor *int64, targetApID, reason string, expires *time.Time) error {
	params := moderationdb.InsertModerationLogParams{
		CommunityID: communityID, ModeratorID: moderatorID, Action: action,
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
		return Target{ApID: p.ApID, Ref: p.Ref, Community: p.CommunityID, AuthorID: p.AuthorID, Local: p.Local}, false, nil
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
	return Target{ApID: c.ApID, Ref: c.Ref, Community: c.CommunityID, AuthorID: c.AuthorID, PostApID: c.PostApID, Local: c.Local}, true, nil
}

// mayModerate allows a community's moderators, and a site admin acting on local
// content in a remote community.
func (s *Service) mayModerate(ctx context.Context, q *moderationdb.Queries, actor actors.Actor, communityID int64, local bool) error {
	err := moderator(ctx, q, communityID, actor.ID)
	if err == nil {
		return nil
	}
	if !errors.Is(err, ErrNotModerator) || !local || s.IsAdmin == nil {
		return err
	}
	admin, aerr := s.IsAdmin(ctx, actor)
	if aerr != nil {
		return aerr
	}
	if !admin {
		return err
	}
	return nil
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
		if err := s.mayModerate(ctx, q, actor, t.Community, t.Local); err != nil {
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
		return writeLog(ctx, q, &t.Community, actor.ID, action, &t.AuthorID, objectApID, reason, nil)
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
		if err := s.mayModerate(ctx, q, actor, t.Community, t.Local); err != nil {
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
		return writeLog(ctx, q, &t.Community, actor.ID, action, &t.AuthorID, objectApID, "", nil)
	})
	return t, err
}

// ErrNotOwner is returned when someone other than an owner appoints a moderator.
var ErrNotOwner = errors.New("only a community's owner can appoint moderators")

// SetRole makes a member a moderator, or a moderator a member again. Only an
// owner can, and the change is in the moderation log.
func (s *Service) SetRole(ctx context.Context, actor actors.Actor, communityID, targetID int64, moderator bool) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		role, err := q.RoleOf(ctx, moderationdb.RoleOfParams{CommunityID: communityID, ActorID: actor.ID})
		if errors.Is(err, pgx.ErrNoRows) || (err == nil && role != "owner") {
			return ErrNotOwner
		}
		if err != nil {
			return fmt.Errorf("owner: %w", err)
		}
		if targetID == actor.ID {
			return ErrNotOwner
		}
		if _, err := q.RoleOf(ctx, moderationdb.RoleOfParams{CommunityID: communityID, ActorID: targetID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		} else if err != nil {
			return fmt.Errorf("member: %w", err)
		}
		next, action := "member", ActionRemoveModerator
		if moderator {
			next, action = "moderator", ActionAddModerator
		}
		if err := q.SetMembershipRole(ctx, moderationdb.SetMembershipRoleParams{
			CommunityID: communityID, ActorID: targetID, Role: next,
		}); err != nil {
			return fmt.Errorf("set role: %w", err)
		}
		if err := writeLog(ctx, q, &communityID, actor.ID, action, &targetID, "", "", nil); err != nil {
			return err
		}
		if s.SendRole == nil {
			return nil
		}
		person, err := actors.ByID(ctx, tx, targetID)
		if err != nil {
			return fmt.Errorf("person %d: %w", targetID, err)
		}
		return s.SendRole(ctx, tx, actor, communityID, person.ApID, moderator)
	})
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
		if err := s.mayModerate(ctx, q, actor, t.Community, t.Local); err != nil {
			return err
		}
		if _, err := q.SetPostLocked(ctx, moderationdb.SetPostLockedParams{ApID: postApID, Locked: locked}); err != nil {
			return fmt.Errorf("lock post: %w", err)
		}
		action := ActionUnlock
		if locked {
			action = ActionLock
		}
		if err := writeLog(ctx, q, &t.Community, actor.ID, action, &t.AuthorID, postApID, "", nil); err != nil {
			return err
		}
		if s.SendLock == nil {
			return nil
		}
		return s.SendLock(ctx, tx, actor, t.Community, postApID, locked)
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
		if err := outranks(ctx, q, communityID, actor.ID, targetID); err != nil {
			return err
		}
		if _, err := q.InsertBan(ctx, moderationdb.InsertBanParams{CommunityID: &communityID, ActorID: targetID, ExpiresAt: expires}); err != nil {
			return fmt.Errorf("ban: %w", err)
		}
		if err := q.DeleteMembershipForBan(ctx, moderationdb.DeleteMembershipForBanParams{CommunityID: communityID, ActorID: targetID}); err != nil {
			return fmt.Errorf("remove member: %w", err)
		}
		if err := writeLog(ctx, q, &communityID, actor.ID, ActionBan, &targetID, "", reason, expires); err != nil {
			return err
		}
		return s.banActivity(ctx, tx, actor, communityID, targetID, true)
	})
}

// outranks refuses a ban of an owner, and a ban of a moderator by anyone but an owner.
func outranks(ctx context.Context, q *moderationdb.Queries, communityID, actorID, targetID int64) error {
	target, err := q.RoleOf(ctx, moderationdb.RoleOfParams{CommunityID: communityID, ActorID: targetID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && target == "member") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("role of %d: %w", targetID, err)
	}
	actor, err := q.RoleOf(ctx, moderationdb.RoleOfParams{CommunityID: communityID, ActorID: actorID})
	if err != nil {
		return fmt.Errorf("role of %d: %w", actorID, err)
	}
	if target == "owner" || actor != "owner" {
		return ErrOutranked
	}
	return nil
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
		if err := writeLog(ctx, q, &communityID, actor.ID, ActionUnban, &targetID, "", "", nil); err != nil {
			return err
		}
		return s.banActivity(ctx, tx, actor, communityID, targetID, false)
	})
}

// banActivity tells a remote person's server that the community banned or
// released them. A local person's ban stays here.
func (s *Service) banActivity(ctx context.Context, tx pgx.Tx, moderator actors.Actor, communityID, targetID int64, blocked bool) error {
	if s.SendBlock == nil {
		return nil
	}
	target, err := actors.ByID(ctx, tx, targetID)
	if err != nil {
		return fmt.Errorf("banned person %d: %w", targetID, err)
	}
	if target.Local {
		return nil
	}
	return s.SendBlock(ctx, tx, moderator, communityID, target.ApID, blocked)
}

// ApplyRemoteBan stores or lifts a ban a remote group made. It writes no log
// row: the group is the one that acted, and its own log is where it shows.
func ApplyRemoteBan(ctx context.Context, db moderationdb.DBTX, communityID, actorID int64, blocked bool) (bool, error) {
	q := moderationdb.New(db)
	_, err := q.ActiveBan(ctx, moderationdb.ActiveBanParams{CommunityID: &communityID, ActorID: actorID})
	active := err == nil
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return false, fmt.Errorf("ban: %w", err)
	}
	if active == blocked {
		return false, nil
	}
	if !blocked {
		if err := q.DeleteBan(ctx, moderationdb.DeleteBanParams{CommunityID: &communityID, ActorID: actorID}); err != nil {
			return false, fmt.Errorf("lift remote ban: %w", err)
		}
		return true, nil
	}
	if _, err := q.InsertBan(ctx, moderationdb.InsertBanParams{CommunityID: &communityID, ActorID: actorID}); err != nil {
		return false, fmt.Errorf("remote ban: %w", err)
	}
	if err := q.DeleteMembershipForBan(ctx, moderationdb.DeleteMembershipForBanParams{CommunityID: communityID, ActorID: actorID}); err != nil {
		return false, fmt.Errorf("remote ban membership: %w", err)
	}
	return true, nil
}

// ApplyRemoteFlag records a report a remote person filed on a post or a
// comment here. The Flag's activity ID keeps a redelivery one report.
func ApplyRemoteFlag(ctx context.Context, db moderationdb.DBTX, communityID, reporterID int64, postID, commentID *int64, reason, flagApID string) (bool, error) {
	_, err := moderationdb.New(db).InsertReport(ctx, moderationdb.InsertReportParams{
		ReporterID: reporterID, PostID: postID, CommentID: commentID,
		CommunityID: communityID, Reason: reason, FlagApID: &flagApID,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remote flag: %w", err)
	}
	return true, nil
}

// Report is one report as moderators see it, with who reported it.
type Report struct {
	ID          int64     `json:"id"`
	Reason      string    `json:"reason"`
	State       string    `json:"state"`
	CreatedAt   time.Time `json:"createdAt"`
	Reporter    string    `json:"reporter" doc:"Who reported it; shown to moderators only"`
	Community   string    `json:"community" doc:"Handle of the community the content is in"`
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
		if banned, err := Banned(ctx, tx, params.CommunityID, reporter.ID); err != nil {
			return err
		} else if banned {
			return ErrBanned
		}
		n, err := q.ReportsSince(ctx, moderationdb.ReportsSinceParams{ReporterID: reporter.ID, Since: time.Now().Add(-24 * time.Hour)})
		if err != nil {
			return fmt.Errorf("report count: %w", err)
		}
		if int(n) >= s.Tuning.ReportsPerDay {
			return ErrReportLimit
		}
		r, err := q.InsertReport(ctx, params)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrReported
		}
		if err != nil {
			return fmt.Errorf("report: %w", err)
		}
		id = r.ID
		if s.SendFlag == nil {
			return nil
		}
		flagApID, err := s.SendFlag(ctx, tx, params.CommunityID, reporter, objectApID, reason)
		if err != nil {
			return err
		}
		if flagApID == "" {
			return nil
		}
		if err := q.SetReportFlag(ctx, moderationdb.SetReportFlagParams{ID: id, FlagApID: &flagApID}); err != nil {
			return fmt.Errorf("report flag: %w", err)
		}
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
			Reporter:  s.Local.Handle(r.ReporterName, r.ReporterDomain),
			Community: s.Local.Handle(r.CommunityName, r.CommunityDomain),
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

// SiteQueue lists every open report on local content, for site admins.
func (s *Service) SiteQueue(ctx context.Context, actor actors.Actor) ([]Report, error) {
	if err := s.requireAdmin(ctx, actor); err != nil {
		return nil, err
	}
	rows, err := moderationdb.New(s.Pool).SiteReportQueue(ctx, s.Tuning.ReportQueueSize)
	if err != nil {
		return nil, fmt.Errorf("report queue: %w", err)
	}
	out := make([]Report, 0, len(rows))
	for _, r := range rows {
		rep := Report{
			ID: r.ID, Reason: r.Reason, State: r.State, CreatedAt: r.CreatedAt,
			Reporter:  s.Local.Handle(r.ReporterName, r.ReporterDomain),
			Community: s.Local.Handle(r.CommunityName, r.CommunityDomain),
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
		report, err := q.ReportForModeration(ctx, reportID)
		if errors.Is(err, pgx.ErrNoRows) {
			return ErrNoObject
		}
		if err != nil {
			return fmt.Errorf("report: %w", err)
		}
		if err := s.mayModerate(ctx, q, actor, report.CommunityID, report.Local); err != nil {
			return err
		}
		if _, err := q.ResolveReport(ctx, moderationdb.ResolveReportParams{ID: reportID, State: state, ResolvedBy: &actor.ID}); errors.Is(err, pgx.ErrNoRows) {
			return ErrAlreadyDone
		} else if err != nil {
			return fmt.Errorf("resolve report: %w", err)
		}
		return writeLog(ctx, q, &report.CommunityID, actor.ID, action, nil, "", "", nil)
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
