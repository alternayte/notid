package communities

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
)

// Service serves the community operations. Federation supplies the two
// functions that reach peers.
type Service struct {
	Pool *pgxpool.Pool
	// Resolve finds a local or remote actor by URL or name@host handle.
	Resolve func(ctx context.Context, ref string) (actors.Actor, error)
	// SendFollow queues a Follow of a remote group and returns its AP ID.
	SendFollow func(ctx context.Context, tx pgx.Tx, person, group actors.Actor) (string, error)
}

type followBody struct {
	Community string `json:"community" minLength:"3" maxLength:"500" doc:"Community URL or name@host handle"`
}

type Membership struct {
	Community string `json:"community" doc:"AP ID of the community"`
	State     string `json:"state" enum:"pending,accepted" doc:"pending until the community's host accepts the follow"`
}

func (s *Service) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "followCommunity",
		Method:      http.MethodPost,
		Path:        "/api/communities/follow",
		Summary:     "Join a community on this site or on another server",
		Tags:        []string{"communities"},
		Errors:      []int{http.StatusUnauthorized, http.StatusNotFound, http.StatusUnprocessableEntity},
	}, func(ctx context.Context, in *struct{ Body followBody }) (*struct{ Body Membership }, error) {
		person := accounts.ActorFrom(ctx)
		if person == nil {
			return nil, huma.Error401Unauthorized("sign in first")
		}
		m, err := s.Follow(ctx, *person, in.Body.Community)
		if errors.Is(err, ErrNotACommunity) {
			return nil, huma.Error404NotFound("No community found at that address.")
		}
		if err != nil {
			return nil, err
		}
		return &struct{ Body Membership }{m}, nil
	})
}

var ErrNotACommunity = errors.New("the address names no community")

// Follow makes person a member of the community ref names. A local community
// accepts at once; a remote one stays pending until its Accept arrives.
func (s *Service) Follow(ctx context.Context, person actors.Actor, ref string) (Membership, error) {
	g, err := s.Resolve(ctx, ref)
	if err != nil || g.Kind != "group" {
		return Membership{}, fmt.Errorf("%q: %w", ref, ErrNotACommunity)
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Membership{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := communitiesdb.New(tx)
	c, err := q.CommunityByActorID(ctx, g.ID)
	if errors.Is(err, pgx.ErrNoRows) && !g.Local {
		c, err = q.InsertRemoteCommunity(ctx, communitiesdb.InsertRemoteCommunityParams{
			ActorID: g.ID, Domain: g.Domain, Name: g.Name, Title: g.DisplayName, Description: g.Summary,
		})
	}
	if err != nil {
		return Membership{}, fmt.Errorf("community of %s: %w", g.ApID, err)
	}
	state, followID := "accepted", (*string)(nil)
	if !g.Local {
		id, err := s.SendFollow(ctx, tx, person, g)
		if err != nil {
			return Membership{}, err
		}
		state, followID = "pending", &id
	}
	m, err := q.UpsertMembership(ctx, communitiesdb.UpsertMembershipParams{
		CommunityID: c.ID, ActorID: person.ID, State: state, FollowApID: followID,
	})
	if err != nil {
		return Membership{}, fmt.Errorf("membership: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Membership{}, fmt.Errorf("commit: %w", err)
	}
	return Membership{Community: g.ApID, State: m.State}, nil
}
