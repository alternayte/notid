package communities

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
	"github.com/alternayte/notid/internal/moderation"
	"github.com/alternayte/notid/internal/platform"
	"github.com/alternayte/notid/internal/platform/media"
)

// Service serves the community operations. Federation supplies the functions
// that reach peers.
type Service struct {
	Pool     *pgxpool.Pool
	Local    actors.Local
	Markdown *platform.Markdown
	// MediaURL is where an uploaded image with a storage key is served.
	MediaURL func(key string) string
	// Resolve finds a local or remote actor by URL or name@host handle.
	Resolve func(ctx context.Context, ref string) (actors.Actor, error)
	// SendFollow queues a Follow of a remote group and returns its AP ID.
	SendFollow func(ctx context.Context, tx pgx.Tx, person, group actors.Actor) (string, error)
	// SendUnfollow queues an Undo of the Follow with the given AP ID.
	SendUnfollow func(ctx context.Context, tx pgx.Tx, person actors.Actor, groupApID, followApID string) error
}

type addressBody struct {
	Community string `json:"community" minLength:"3" maxLength:"500" doc:"Community handle (name, or name@host), URL or AP ID"`
}

type Membership struct {
	Community string `json:"community" doc:"AP ID of the community"`
	State     string `json:"state" enum:"pending,accepted,none" doc:"pending until the community's host accepts the follow; none after leaving"`
}

type MyMembership struct {
	State string `json:"state" enum:"pending,accepted"`
	Role  string `json:"role" enum:"member,moderator,owner"`
}

// Summary is a community in a list.
type Summary struct {
	Handle          string        `json:"handle" doc:"name for communities on this site's domain, else name@domain"`
	ApID            string        `json:"apId"`
	Name            string        `json:"name"`
	Domain          string        `json:"domain"`
	Title           string        `json:"title"`
	DescriptionHTML string        `json:"descriptionHtml" doc:"Sanitized HTML from the Markdown description"`
	Local           bool          `json:"local" doc:"true when the community lives on this installation"`
	Members         int64         `json:"members" doc:"Members known to this installation"`
	NSFW            bool          `json:"nsfw"`
	Theme           Theme         `json:"theme"`
	BannerURL       string        `json:"bannerUrl" doc:"URL of the theme's banner image; empty without one"`
	Membership      *MyMembership `json:"membership,omitempty" doc:"The signed-in person's membership, if any"`
}

type Person struct {
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Role        string `json:"role" enum:"owner,moderator"`
}

// Detail is one community with its about page.
type Detail struct {
	Summary
	Description    string   `json:"description" doc:"Markdown"`
	Rules          []string `json:"rules" nullable:"false"`
	Moderators     []Person `json:"moderators" nullable:"false"`
	AppealsContact string   `json:"appealsContact"`
	HoldNewPosts   bool     `json:"holdNewPosts" doc:"A new person's posts wait for a moderator publication"`
	State          string   `json:"state" enum:"active,frozen,departed,arriving"`
}

type SettingsBody struct {
	Title          string   `json:"title" minLength:"1" maxLength:"100"`
	Description    string   `json:"description" maxLength:"10000" doc:"Markdown"`
	Rules          []string `json:"rules" maxItems:"15" doc:"One rule per item, in order"`
	Theme          Theme    `json:"theme"`
	AppealsContact string   `json:"appealsContact" maxLength:"200" doc:"Where members appeal a removal: an email address or URL"`
	NSFW           bool     `json:"nsfw"`
	HoldNewPosts   bool     `json:"holdNewPosts" doc:"Hold a new person's posts until a moderator publishes them"`
}

func (b SettingsBody) settings() Settings {
	return Settings{
		Title: b.Title, Description: b.Description, Rules: b.Rules, Theme: b.Theme,
		AppealsContact: b.AppealsContact, NSFW: b.NSFW, HoldNewPosts: b.HoldNewPosts,
	}
}

type createBody struct {
	Name string `json:"name" pattern:"^[a-z][a-z0-9_]{2,19}$" doc:"Fixed forever; part of the community's address"`
	SettingsBody
}

type handlePath struct {
	Handle string `path:"handle" maxLength:"300"`
}

type list struct {
	Communities []Summary `json:"communities" nullable:"false"`
}

func (s *Service) Register(api huma.API) {
	tags := []string{"communities"}
	huma.Register(api, huma.Operation{
		OperationID: "createCommunity", Method: http.MethodPost, Path: "/api/communities",
		Summary: "Create a community on this site", Tags: tags, DefaultStatus: http.StatusCreated,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *struct{ Body createBody }) (*struct{ Body Detail }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		var c Community
		err = s.inTx(ctx, func(tx pgx.Tx) error {
			c, err = CreateLocal(ctx, tx, s.Local, person, in.Body.Name, in.Body.settings())
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		d, err := s.Detail(ctx, s.Local.AppDomain, c.Name, &person)
		return &struct{ Body Detail }{d}, err
	})

	huma.Register(api, huma.Operation{
		OperationID: "discoverCommunities", Method: http.MethodGet, Path: "/api/communities",
		Summary: "List the communities on this site and the reviewed directory, largest first", Tags: tags,
	}, func(ctx context.Context, in *struct {
		Limit  int32 `query:"limit" minimum:"1" maximum:"100" default:"50"`
		Offset int32 `query:"offset" minimum:"0" default:"0"`
	}) (*struct{ Body list }, error) {
		var viewer *int64
		if a := accounts.ActorFrom(ctx); a != nil {
			viewer = &a.ID
		}
		rows, err := communitiesdb.New(s.Pool).Discover(ctx, communitiesdb.DiscoverParams{ViewerID: viewer, Lim: in.Limit, Off: in.Offset})
		if err != nil {
			return nil, fmt.Errorf("discover: %w", err)
		}
		out := list{Communities: make([]Summary, 0, len(rows))}
		for _, r := range rows {
			out.Communities = append(out.Communities, s.summary(communitiesdb.JoinedRow{
				ID: r.ID, Domain: r.Domain, Name: r.Name, Title: r.Title, Description: r.Description, Theme: r.Theme,
				Nsfw: r.Nsfw, ApID: r.ApID, Local: r.Local, Members: r.Members,
			}, membership(r.MyState, r.MyRole)))
		}
		return &struct{ Body list }{out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "searchCommunities", Method: http.MethodGet, Path: "/api/search/communities",
		Summary: "Search communities on this site, in the directory, or followed by someone here, largest first", Tags: tags,
	}, func(ctx context.Context, in *struct {
		Q string `query:"q" minLength:"1" maxLength:"200" doc:"Words to find; quotes match a phrase, a leading - excludes a word"`
	}) (*struct{ Body list }, error) {
		var viewer *int64
		if a := accounts.ActorFrom(ctx); a != nil {
			viewer = &a.ID
		}
		rows, err := communitiesdb.New(s.Pool).SearchCommunities(ctx, communitiesdb.SearchCommunitiesParams{ViewerID: viewer, Query: in.Q, Lim: 20})
		if err != nil {
			return nil, fmt.Errorf("search communities: %w", err)
		}
		out := list{Communities: make([]Summary, 0, len(rows))}
		for _, r := range rows {
			out.Communities = append(out.Communities, s.summary(communitiesdb.JoinedRow{
				ID: r.ID, Domain: r.Domain, Name: r.Name, Title: r.Title, Description: r.Description, Theme: r.Theme,
				Nsfw: r.Nsfw, ApID: r.ApID, Local: r.Local, Members: r.Members,
			}, membership(r.MyState, r.MyRole)))
		}
		return &struct{ Body list }{out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "myCommunities", Method: http.MethodGet, Path: "/api/me/communities",
		Summary: "List the communities the signed-in person joined or asked to join", Tags: tags,
		Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body list }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		rows, err := communitiesdb.New(s.Pool).Joined(ctx, person.ID)
		if err != nil {
			return nil, fmt.Errorf("joined: %w", err)
		}
		out := list{Communities: make([]Summary, 0, len(rows))}
		for _, r := range rows {
			out.Communities = append(out.Communities, s.summary(r, &MyMembership{State: r.MyState, Role: r.MyRole}))
		}
		return &struct{ Body list }{out}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "community", Method: http.MethodGet, Path: "/api/communities/{handle}",
		Summary: "Read a community and its about page", Tags: tags, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *handlePath) (*struct{ Body Detail }, error) {
		domain, name := s.Local.SplitHandle(in.Handle)
		d, err := s.Detail(ctx, domain, name, accounts.ActorFrom(ctx))
		if err != nil {
			return nil, apiError(err)
		}
		return &struct{ Body Detail }{d}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "updateCommunity", Method: http.MethodPut, Path: "/api/communities/{handle}",
		Summary: "Change a community's settings, rules and theme", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *struct {
		Handle string `path:"handle" maxLength:"300"`
		Body   SettingsBody
	}) (*struct{ Body Detail }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		domain, name := s.Local.SplitHandle(in.Handle)
		row, err := communitiesdb.New(s.Pool).CommunityByHandle(ctx, communitiesdb.CommunityByHandleParams{Domain: domain, Name: name})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, apiError(ErrNotFound)
		}
		if err != nil {
			return nil, fmt.Errorf("community %s: %w", in.Handle, err)
		}
		err = s.inTx(ctx, func(tx pgx.Tx) error {
			_, err := Update(ctx, tx, person, row.ID, in.Body.settings())
			return err
		})
		if err != nil {
			return nil, apiError(err)
		}
		d, err := s.Detail(ctx, domain, name, &person)
		return &struct{ Body Detail }{d}, err
	})

	huma.Register(api, huma.Operation{
		OperationID: "lookupCommunity", Method: http.MethodPost, Path: "/api/communities/lookup",
		Summary: "Find a community on this site or on another server by its address", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound},
	}, func(ctx context.Context, in *struct{ Body addressBody }) (*struct{ Body Detail }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		c, err := s.Lookup(ctx, in.Body.Community)
		if err != nil {
			return nil, apiError(err)
		}
		d, err := s.Detail(ctx, c.Domain, c.Name, &person)
		return &struct{ Body Detail }{d}, err
	})

	huma.Register(api, huma.Operation{
		OperationID: "followCommunity", Method: http.MethodPost, Path: "/api/communities/follow",
		Summary: "Join a community on this site or on another server", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *struct{ Body addressBody }) (*struct{ Body Membership }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		m, err := s.Follow(ctx, person, in.Body.Community)
		if err != nil {
			return nil, apiError(err)
		}
		return &struct{ Body Membership }{m}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "leaveCommunity", Method: http.MethodPost, Path: "/api/communities/leave",
		Summary: "Leave a community", Tags: tags,
		Errors: []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusConflict},
	}, func(ctx context.Context, in *struct{ Body addressBody }) (*struct{ Body Membership }, error) {
		person, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		m, err := s.Leave(ctx, person, in.Body.Community)
		if err != nil {
			return nil, apiError(err)
		}
		return &struct{ Body Membership }{m}, nil
	})
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
	case errors.Is(err, ErrNotFound), errors.Is(err, ErrNotACommunity):
		return huma.Error404NotFound("No community found at that address.")
	case errors.Is(err, ErrNewAccount):
		return huma.Error403Forbidden("New accounts cannot create communities yet. Take part for a while first.")
	case errors.Is(err, ErrNotModerator):
		return huma.Error403Forbidden("Only the community's owners and moderators can change it.")
	case errors.Is(err, ErrNotActive):
		return huma.Error409Conflict("This community is moving and is read-only for now.")
	case errors.Is(err, ErrLastOwner):
		return huma.Error409Conflict("You are the last owner. Add another owner before you leave.")
	case errors.Is(err, actors.ErrNameTaken):
		return huma.Error409Conflict("That name is taken. Choose another.")
	case errors.Is(err, actors.ErrNameInvalid):
		return huma.Error422UnprocessableEntity("A name has 3 to 20 characters: lowercase letters, digits and underscores, starting with a letter.")
	case errors.Is(err, ErrBanned):
		return huma.Error403Forbidden(err.Error())
	case errors.Is(err, ErrAccentUnknown):
		return huma.Error422UnprocessableEntity("Choose one of the listed accents.")
	case errors.Is(err, media.ErrNotYours):
		return huma.Error422UnprocessableEntity("Upload the banner yourself, then save.")
	}
	return err
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

func membership(state, role *string) *MyMembership {
	if state == nil || role == nil {
		return nil
	}
	return &MyMembership{State: *state, Role: *role}
}

func (s *Service) summary(r communitiesdb.JoinedRow, m *MyMembership) Summary {
	theme := parseTheme(r.Theme)
	sum := Summary{
		Handle: s.Local.Handle(r.Name, r.Domain), ApID: r.ApID, Name: r.Name, Domain: r.Domain,
		Title: r.Title, DescriptionHTML: s.Markdown.HTML(r.Description), Local: r.Local,
		Members: r.Members, NSFW: r.Nsfw, Theme: theme, Membership: m,
	}
	if theme.Banner != "" && s.MediaURL != nil {
		sum.BannerURL = s.MediaURL(theme.Banner)
	}
	return sum
}

// Detail reads one community as viewer sees it; viewer may be nil.
func (s *Service) Detail(ctx context.Context, domain, name string, viewer *actors.Actor) (Detail, error) {
	q := communitiesdb.New(s.Pool)
	r, err := q.CommunityByHandle(ctx, communitiesdb.CommunityByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return Detail{}, ErrNotFound
	}
	if err != nil {
		return Detail{}, fmt.Errorf("community %s@%s: %w", name, domain, err)
	}
	var mine *MyMembership
	if viewer != nil {
		m, err := q.MembershipOf(ctx, communitiesdb.MembershipOfParams{CommunityID: r.ID, ActorID: viewer.ID})
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			return Detail{}, fmt.Errorf("membership: %w", err)
		}
		if err == nil {
			mine = &MyMembership{State: m.State, Role: m.Role}
		}
	}
	rules, err := q.Rules(ctx, r.ID)
	if err != nil {
		return Detail{}, fmt.Errorf("rules: %w", err)
	}
	mods, err := q.Moderators(ctx, r.ID)
	if err != nil {
		return Detail{}, fmt.Errorf("moderators: %w", err)
	}
	d := Detail{
		Summary: s.summary(communitiesdb.JoinedRow{
			ID: r.ID, Domain: r.Domain, Name: r.Name, Title: r.Title, Description: r.Description, Theme: r.Theme,
			Nsfw: r.Nsfw, ApID: r.ApID, Local: r.Local, Members: r.Members,
		}, mine),
		Description: r.Description, Rules: rules, Moderators: make([]Person, 0, len(mods)),
		AppealsContact: r.AppealsContact, HoldNewPosts: r.HoldNewPosts, State: r.State,
	}
	if d.Rules == nil {
		d.Rules = []string{}
	}
	for _, m := range mods {
		d.Moderators = append(d.Moderators, Person{Handle: s.Local.Handle(m.Name, m.Domain), DisplayName: m.DisplayName, Role: m.Role})
	}
	return d, nil
}

var ErrNotACommunity = errors.New("the address names no community")

// ErrBanned is returned to a person banned from a community they try to join.
var ErrBanned = errors.New("you are banned from this community")

// address turns a bare local name into a handle Resolve accepts.
func (s *Service) address(ref string) string {
	ref = strings.TrimSpace(ref)
	if !strings.Contains(ref, "://") && !strings.Contains(ref, "@") {
		return strings.TrimPrefix(ref, "!") + "@" + s.Local.AppDomain
	}
	return ref
}

// Lookup resolves an address to a community, and records a remote community
// the first time someone here looks it up.
func (s *Service) Lookup(ctx context.Context, ref string) (Community, error) {
	g, err := s.Resolve(ctx, s.address(ref))
	if err != nil || g.Kind != "group" {
		return Community{}, fmt.Errorf("%q: %w", ref, ErrNotACommunity)
	}
	return s.communityOf(ctx, communitiesdb.New(s.Pool), g)
}

func (s *Service) communityOf(ctx context.Context, q *communitiesdb.Queries, g actors.Actor) (Community, error) {
	c, err := q.CommunityByActorID(ctx, g.ID)
	if errors.Is(err, pgx.ErrNoRows) && !g.Local {
		c, err = q.InsertRemoteCommunity(ctx, communitiesdb.InsertRemoteCommunityParams{
			ActorID: g.ID, Domain: g.Domain, Name: g.Name, Title: g.DisplayName, Description: g.Summary,
		})
	}
	if err != nil {
		return Community{}, fmt.Errorf("community of %s: %w", g.ApID, err)
	}
	return c, nil
}

// Follow makes person a member of the community ref names. A local community
// accepts at once; a remote one stays pending until its Accept arrives.
func (s *Service) Follow(ctx context.Context, person actors.Actor, ref string) (Membership, error) {
	g, err := s.Resolve(ctx, s.address(ref))
	if err != nil || g.Kind != "group" {
		return Membership{}, fmt.Errorf("%q: %w", ref, ErrNotACommunity)
	}
	var m communitiesdb.Membership
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		q := communitiesdb.New(tx)
		c, err := s.communityOf(ctx, q, g)
		if err != nil {
			return err
		}
		if g.Local && c.State != "active" {
			return ErrNotActive
		}
		if banned, err := moderation.Banned(ctx, tx, c.ID, person.ID); err != nil {
			return err
		} else if banned {
			return ErrBanned
		}
		state, followID := "accepted", (*string)(nil)
		if !g.Local {
			id, err := s.SendFollow(ctx, tx, person, g)
			if err != nil {
				return err
			}
			state, followID = "pending", &id
		}
		m, err = q.UpsertMembership(ctx, communitiesdb.UpsertMembershipParams{
			CommunityID: c.ID, ActorID: person.ID, State: state, FollowApID: followID,
		})
		if err != nil {
			return fmt.Errorf("membership: %w", err)
		}
		return nil
	})
	if err != nil {
		return Membership{}, err
	}
	return Membership{Community: g.ApID, State: m.State}, nil
}

// Leave ends person's membership in the community ref names, and undoes the
// Follow of a remote community.
func (s *Service) Leave(ctx context.Context, person actors.Actor, ref string) (Membership, error) {
	g, err := s.Resolve(ctx, s.address(ref))
	if err != nil || g.Kind != "group" {
		return Membership{}, fmt.Errorf("%q: %w", ref, ErrNotACommunity)
	}
	err = s.inTx(ctx, func(tx pgx.Tx) error {
		c, err := communitiesdb.New(tx).CommunityByActorID(ctx, g.ID)
		if errors.Is(err, pgx.ErrNoRows) {
			return nil
		}
		if err != nil {
			return fmt.Errorf("community of %s: %w", g.ApID, err)
		}
		followID, err := Leave(ctx, tx, person, c.ID)
		if err != nil || followID == "" {
			return err
		}
		return s.SendUnfollow(ctx, tx, person, g.ApID, followID)
	})
	if err != nil {
		return Membership{}, err
	}
	return Membership{Community: g.ApID, State: "none"}, nil
}
