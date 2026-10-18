package moderation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/moderation/moderationdb"
)

// Outcome is what a moderation action changed.
type Outcome struct {
	ApID      string `json:"apId"`
	Ref       string `json:"ref"`
	Community string `json:"community" doc:"Handle of the community"`
	PostApID  string `json:"postApId,omitempty" doc:"For a comment: the post it belongs to"`
}

func (s *Service) outcome(t Target) Outcome {
	return Outcome{ApID: t.ApID, Ref: t.Ref, PostApID: t.PostApID}
}

type objectBody struct {
	Object string `json:"object" doc:"AP ID of the post or comment" maxLength:"500"`
	Reason string `json:"reason,omitempty" maxLength:"500" doc:"Shown in the public moderation log"`
}

type lockBody struct {
	Object string `json:"object" doc:"AP ID of the post" maxLength:"500"`
	Locked bool   `json:"locked"`
}

type banBody struct {
	Community string `json:"community" maxLength:"300" doc:"Community handle"`
	Actor     string `json:"actor" maxLength:"300" doc:"Person's handle: name on this site, else name@domain"`
	Reason    string `json:"reason,omitempty" maxLength:"500" doc:"Shown in the public moderation log"`
	Days      int    `json:"days,omitempty" minimum:"0" maximum:"3650" doc:"0 bans until an owner lifts it"`
}

type reportBody struct {
	Object string `json:"object" doc:"AP ID of the post or comment" maxLength:"500"`
	Reason string `json:"reason" minLength:"1" maxLength:"1000"`
}

type resolveBody struct {
	Upheld bool `json:"upheld" doc:"true keeps the report as a real problem, false dismisses it"`
}

type moderatorBody struct {
	Actor string `json:"actor" maxLength:"300" doc:"Person's handle: name on this site, else name@domain"`
	Role  string `json:"role" enum:"moderator,member"`
}

type logOut struct {
	Entries []Entry `json:"entries" nullable:"false"`
}

type queueOut struct {
	Reports []Report `json:"reports" nullable:"false"`
}

type siteBanBody struct {
	Actor  string `json:"actor" maxLength:"300" doc:"Person's handle: name on this site, else name@domain"`
	Reason string `json:"reason,omitempty" maxLength:"500" doc:"Shown in the public moderation log"`
	Days   int    `json:"days,omitempty" minimum:"0" maximum:"3650" doc:"0 bans until a site admin lifts it"`
}

type siteBansOut struct {
	Bans []SiteBanView `json:"bans" nullable:"false"`
}

func signedIn(ctx context.Context) (actors.Actor, error) {
	a := accounts.ActorFrom(ctx)
	if a == nil {
		return actors.Actor{}, huma.Error401Unauthorized("sign in first")
	}
	return *a, nil
}

func apiError(err error) error {
	switch {
	case err == nil:
		return nil
	case errors.Is(err, ErrNoObject):
		return huma.Error404NotFound(err.Error())
	case errors.Is(err, ErrNotModerator), errors.Is(err, ErrNotAdmin), errors.Is(err, ErrNotOwner):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, ErrBanned), errors.Is(err, ErrSelfBan), errors.Is(err, ErrAlreadyDone):
		return huma.Error409Conflict(err.Error())
	}
	return err
}

func (s *Service) Register(api huma.API) {
	tags := []string{"moderation"}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict}
	huma.Register(api, huma.Operation{
		OperationID: "removeObject", Method: http.MethodPost, Path: "/api/moderation/remove",
		Summary: "Remove a post or a comment from a community, and record it in the moderation log",
		Tags:    tags, DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body objectBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		_, err = s.Remove(ctx, a, in.Body.Object, in.Body.Reason)
		return nil, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "restoreObject", Method: http.MethodPost, Path: "/api/moderation/restore",
		Summary: "Restore a removed post or comment", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body objectBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		_, err = s.Restore(ctx, a, in.Body.Object)
		return nil, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "lockPost", Method: http.MethodPost, Path: "/api/moderation/lock",
		Summary: "Lock or unlock replies to a post", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body lockBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		_, err = s.SetLocked(ctx, a, in.Body.Object, in.Body.Locked)
		return nil, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "banActor", Method: http.MethodPost, Path: "/api/moderation/ban",
		Summary: "Ban a person from a community, for a number of days or until lifted", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body banBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		communityID, targetID, err := s.banTargets(ctx, in.Body.Community, in.Body.Actor)
		if err != nil {
			return nil, apiError(err)
		}
		var expires *time.Time
		if in.Body.Days > 0 {
			t := time.Now().Add(time.Duration(in.Body.Days) * 24 * time.Hour)
			expires = &t
		}
		return nil, apiError(s.Ban(ctx, a, communityID, targetID, in.Body.Reason, expires))
	})
	huma.Register(api, huma.Operation{
		OperationID: "unbanActor", Method: http.MethodPost, Path: "/api/moderation/unban",
		Summary: "Lift a ban", Tags: tags, DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body banBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		communityID, targetID, err := s.banTargets(ctx, in.Body.Community, in.Body.Actor)
		if err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(s.Unban(ctx, a, communityID, targetID))
	})
	huma.Register(api, huma.Operation{
		OperationID: "reportObject", Method: http.MethodPost, Path: "/api/reports",
		Summary: "Report a post or a comment to its community's moderators", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *struct{ Body reportBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		_, err = s.File(ctx, a, in.Body.Object, in.Body.Reason)
		return nil, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "reportQueue", Method: http.MethodGet, Path: "/api/communities/{handle}/reports",
		Summary: "List a community's open reports for its moderators", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		Handle string `path:"handle" maxLength:"300"`
	}) (*struct{ Body queueOut }, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		reports, err := s.Queue(ctx, a, in.Handle)
		return &struct{ Body queueOut }{queueOut{Reports: reports}}, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "resolveReport", Method: http.MethodPost, Path: "/api/communities/{handle}/reports/{id}",
		Summary: "Close a report as upheld or dismissed", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct {
		Handle string `path:"handle" maxLength:"300"`
		ID     int64  `path:"id" minimum:"1"`
		Body   resolveBody
	}) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		return nil, apiError(s.Resolve(ctx, a, in.ID, in.Body.Upheld))
	})
	huma.Register(api, huma.Operation{
		OperationID: "moderationLog", Method: http.MethodGet, Path: "/api/communities/{handle}/log",
		Summary: "Read a community's public moderation log, newest first", Tags: tags,
		Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		Handle string `path:"handle" maxLength:"300"`
	}) (*struct{ Body logOut }, error) {
		entries, err := s.Log(ctx, in.Handle)
		return &struct{ Body logOut }{logOut{Entries: entries}}, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "setModerator", Method: http.MethodPost, Path: "/api/communities/{handle}/moderators",
		Summary: "Make a member a moderator, or a moderator a member again. Owners appoint", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct {
		Handle string `path:"handle" maxLength:"300"`
		Body   moderatorBody
	}) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		communityID, targetID, err := s.banTargets(ctx, in.Handle, in.Body.Actor)
		if err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(s.SetRole(ctx, a, communityID, targetID, in.Body.Role == "moderator"))
	})
	huma.Register(api, huma.Operation{
		OperationID: "about", Method: http.MethodGet, Path: "/api/about",
		Summary: "Read the public moderation counts", Tags: []string{"about"},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body Transparency }, error) {
		t, err := s.PublicCounts(ctx)
		if err != nil {
			return nil, err
		}
		return &struct{ Body Transparency }{t}, nil
	})
	s.registerSiteAdmin(api)
}

// registerSiteAdmin adds the site-wide operations. Only auth-all admins pass.
func (s *Service) registerSiteAdmin(api huma.API) {
	tags := []string{"admin"}
	errs := []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict}
	huma.Register(api, huma.Operation{
		OperationID: "siteBan", Method: http.MethodPost, Path: "/api/admin/bans",
		Summary: "Ban a person from the whole site", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body siteBanBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		target, err := s.SitePerson(ctx, in.Body.Actor)
		if err != nil {
			return nil, apiError(err)
		}
		var expires *time.Time
		if in.Body.Days > 0 {
			t := time.Now().Add(time.Duration(in.Body.Days) * 24 * time.Hour)
			expires = &t
		}
		return nil, apiError(s.SiteBan(ctx, a, target, in.Body.Reason, expires))
	})
	huma.Register(api, huma.Operation{
		OperationID: "siteUnban", Method: http.MethodPost, Path: "/api/admin/bans/lift",
		Summary: "Lift a site ban", Tags: tags,
		DefaultStatus: http.StatusNoContent, Errors: errs,
	}, func(ctx context.Context, in *struct{ Body siteBanBody }) (*struct{}, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		target, err := s.SitePerson(ctx, in.Body.Actor)
		if err != nil {
			return nil, apiError(err)
		}
		return nil, apiError(s.SiteUnban(ctx, a, target))
	})
	huma.Register(api, huma.Operation{
		OperationID: "siteBans", Method: http.MethodGet, Path: "/api/admin/bans",
		Summary: "List the active site bans", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body siteBansOut }, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		bans, err := s.SiteBans(ctx, a)
		return &struct{ Body siteBansOut }{siteBansOut{Bans: bans}}, apiError(err)
	})
	huma.Register(api, huma.Operation{
		OperationID: "siteReports", Method: http.MethodGet, Path: "/api/admin/reports",
		Summary: "List open reports on content written on this site", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body queueOut }, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		reports, err := s.SiteQueue(ctx, a)
		return &struct{ Body queueOut }{queueOut{Reports: reports}}, apiError(err)
	})
}

// banTargets reads the community and the person a ban names.
func (s *Service) banTargets(ctx context.Context, community, handle string) (communityID, actorID int64, err error) {
	q := moderationdb.New(s.Pool)
	domain, name := s.Local.SplitHandle(community)
	communityID, err = q.CommunityIDByHandle(ctx, moderationdb.CommunityIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNoObject
	}
	if err != nil {
		return 0, 0, fmt.Errorf("community: %w", err)
	}
	actorDomain, actorName := s.Local.SplitHandle(handle)
	actorID, err = q.ActorIDByHandle(ctx, moderationdb.ActorIDByHandleParams{Domain: actorDomain, Name: actorName})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, 0, ErrNoObject
	}
	if err != nil {
		return 0, 0, fmt.Errorf("person: %w", err)
	}
	return communityID, actorID, nil
}
