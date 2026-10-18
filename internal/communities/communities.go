// Package communities owns communities, memberships, rules, themes, community domains and discovery.
package communities

import (
	"context"
	"errors"
	"fmt"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
)

type Community = communitiesdb.Community

// Create makes a community and its group actor on domain in one transaction.
func Create(ctx context.Context, tx pgx.Tx, local actors.Local, domain, name, title, description string) (Community, actors.Actor, error) {
	g, err := local.CreateGroup(ctx, tx, domain, name, title, description)
	if err != nil {
		return Community{}, actors.Actor{}, err
	}
	c, err := communitiesdb.New(tx).InsertCommunity(ctx, communitiesdb.InsertCommunityParams{
		ActorID: g.ID, Domain: domain, Name: name, Title: title, Description: description,
	})
	if err != nil {
		return Community{}, actors.Actor{}, fmt.Errorf("insert community: %w", err)
	}
	if domain != local.AppDomain {
		// The operator creates it from the command line, so the domain counts as verified.
		app, err := local.Application(ctx, tx)
		if err != nil {
			return Community{}, actors.Actor{}, err
		}
		if err := communitiesdb.New(tx).InsertVerifiedDomain(ctx, communitiesdb.InsertVerifiedDomainParams{
			Domain: domain, TxtToken: "operator", CreatedBy: app.ID, CommunityID: &c.ID,
		}); err != nil {
			return Community{}, actors.Actor{}, fmt.Errorf("community domain %s: %w", domain, err)
		}
	}
	return c, g, nil
}

// DomainState returns the state of the community on a verified community
// domain here, or "" when host is not one.
func DomainState(pool communitiesdb.DBTX) func(ctx context.Context, host string) (string, error) {
	return func(ctx context.Context, host string) (string, error) {
		state, err := communitiesdb.New(pool).DomainState(ctx, host)
		if errors.Is(err, pgx.ErrNoRows) {
			return "", nil
		}
		return state, err
	}
}
