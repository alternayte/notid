package communities

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
	"github.com/alternayte/notid/internal/platform/media"
)

var (
	ErrNotFound      = errors.New("no community with that handle")
	ErrNewAccount    = errors.New("new accounts cannot create communities yet")
	ErrNotModerator  = errors.New("only the community's owners and moderators can change it")
	ErrLastOwner     = errors.New("the last owner cannot leave the community")
	ErrNotActive     = errors.New("the community is moving and is read-only for now")
	ErrNotLocal      = errors.New("this community is hosted on another site and changes there")
	ErrAccentUnknown = errors.New("unknown accent")
)

// Accents are the community accent presets. Each has a light and a dark value
// in the web app's tokens that keep text on it readable; a free colour could not.
var Accents = []string{"vermilion", "moss", "teal", "cobalt", "plum", "ochre"}

type Theme struct {
	Accent string `json:"accent,omitempty" enum:"vermilion,moss,teal,cobalt,plum,ochre"`
	Banner string `json:"banner,omitempty" maxLength:"40" doc:"Key of an image uploaded through /api/media"`
}

func parseTheme(b []byte) Theme {
	var t Theme
	_ = json.Unmarshal(b, &t) // the column only ever holds what Settings wrote
	return t
}

// Settings is what owners and moderators change about a community.
type Settings struct {
	Title          string
	Description    string
	Rules          []string
	Theme          Theme
	AppealsContact string
	NSFW           bool
	// HoldNewPosts keeps a new person's posts in a moderator queue.
	HoldNewPosts bool
}

// CreateLocal makes a community on the app domain with person as its owner.
func CreateLocal(ctx context.Context, tx pgx.Tx, local actors.Local, person actors.Actor, name string, s Settings) (Community, error) {
	if person.Trust == nil || *person.Trust != "trusted" {
		return Community{}, ErrNewAccount
	}
	if s.Title == "" {
		s.Title = name
	}
	c, _, err := Create(ctx, tx, local, local.AppDomain, name, s.Title, s.Description)
	if err != nil {
		return Community{}, err
	}
	q := communitiesdb.New(tx)
	if err := q.InsertOwner(ctx, communitiesdb.InsertOwnerParams{CommunityID: c.ID, ActorID: person.ID}); err != nil {
		return Community{}, fmt.Errorf("owner: %w", err)
	}
	return update(ctx, tx, c, person.ID, s)
}

// ApplyRemoteUpdate stores a remote group's new title, description and NSFW flag.
func ApplyRemoteUpdate(ctx context.Context, db communitiesdb.DBTX, apID, title, description string, nsfw bool) (bool, error) {
	_, err := communitiesdb.New(db).UpdateRemoteCommunity(ctx, communitiesdb.UpdateRemoteCommunityParams{
		ApID: apID, Title: title, Description: description, Nsfw: nsfw,
	})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("remote group %s: %w", apID, err)
	}
	return true, nil
}

// Update changes a community's settings, rules and theme.
func Update(ctx context.Context, tx pgx.Tx, editor actors.Actor, communityID int64, s Settings) (Community, error) {
	q := communitiesdb.New(tx)
	m, err := q.MembershipOf(ctx, communitiesdb.MembershipOfParams{CommunityID: communityID, ActorID: editor.ID})
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && (m.State != "accepted" || m.Role == "member")) {
		return Community{}, ErrNotModerator
	}
	if err != nil {
		return Community{}, fmt.Errorf("membership: %w", err)
	}
	c, err := q.CommunityByID(ctx, communityID)
	if err != nil {
		return Community{}, fmt.Errorf("community %d: %w", communityID, err)
	}
	// A remote community changes on its own installation; this copy follows its Updates.
	if g, err := actors.ByID(ctx, tx, c.ActorID); err != nil {
		return Community{}, fmt.Errorf("group %d: %w", c.ActorID, err)
	} else if !g.Local {
		return Community{}, ErrNotLocal
	}
	if c.State != "active" {
		return Community{}, ErrNotActive
	}
	return update(ctx, tx, c, editor.ID, s)
}

func update(ctx context.Context, tx pgx.Tx, c Community, editorID int64, s Settings) (Community, error) {
	if s.Theme.Accent != "" && !slices.Contains(Accents, s.Theme.Accent) {
		return Community{}, fmt.Errorf("%q: %w", s.Theme.Accent, ErrAccentUnknown)
	}
	// A new banner must be the editor's own upload; an unchanged one stays whoever uploaded it.
	if b := s.Theme.Banner; b != "" && b != parseTheme(c.Theme).Banner {
		if _, err := media.Owned(ctx, tx, b, editorID); err != nil {
			return Community{}, err
		}
	}
	theme, err := json.Marshal(s.Theme)
	if err != nil {
		return Community{}, fmt.Errorf("theme: %w", err)
	}
	q := communitiesdb.New(tx)
	c, err = q.UpdateCommunity(ctx, communitiesdb.UpdateCommunityParams{
		ID: c.ID, Title: s.Title, Description: s.Description, Theme: theme,
		AppealsContact: s.AppealsContact, Nsfw: s.NSFW, HoldNewPosts: s.HoldNewPosts,
	})
	if err != nil {
		return Community{}, fmt.Errorf("update community: %w", err)
	}
	if err := q.DeleteRules(ctx, c.ID); err != nil {
		return Community{}, fmt.Errorf("clear rules: %w", err)
	}
	position := int32(0)
	for _, r := range s.Rules {
		if r = strings.TrimSpace(r); r == "" {
			continue
		}
		position++
		if err := q.InsertRule(ctx, communitiesdb.InsertRuleParams{CommunityID: c.ID, Position: position, Text: r}); err != nil {
			return Community{}, fmt.Errorf("rule %d: %w", position, err)
		}
	}
	if err := actors.UpdateLocalGroupProfile(ctx, tx, c.ActorID, c.Title, c.Description); err != nil {
		return Community{}, err
	}
	return c, nil
}

// Leave ends person's membership. For a remote community it returns the AP ID
// of the Follow to undo, or "" when there is none.
func Leave(ctx context.Context, tx pgx.Tx, person actors.Actor, communityID int64) (string, error) {
	q := communitiesdb.New(tx)
	c, err := q.CommunityByID(ctx, communityID)
	if err != nil {
		return "", fmt.Errorf("community %d: %w", communityID, err)
	}
	local, err := q.CommunityIsLocal(ctx, communityID)
	if err != nil {
		return "", fmt.Errorf("community %d: %w", communityID, err)
	}
	if local && c.State != "active" {
		return "", ErrNotActive
	}
	m, err := q.DeleteMembership(ctx, communitiesdb.DeleteMembershipParams{CommunityID: communityID, ActorID: person.ID})
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", fmt.Errorf("leave: %w", err)
	}
	if m.Role == "owner" {
		owners, err := q.CountOwners(ctx, communityID)
		if err != nil {
			return "", fmt.Errorf("count owners: %w", err)
		}
		if owners == 0 {
			return "", ErrLastOwner
		}
	}
	if local || m.FollowApID == nil {
		return "", nil
	}
	return *m.FollowApID, nil
}

// SyncRemoteNSFW copies a freshly fetched remote group's sensitive flag to its community.
func SyncRemoteNSFW(ctx context.Context, db communitiesdb.DBTX, actorID int64) error {
	if err := communitiesdb.New(db).SyncRemoteCommunityNSFW(ctx, actorID); err != nil {
		return fmt.Errorf("nsfw of group %d: %w", actorID, err)
	}
	return nil
}
