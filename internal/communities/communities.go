// Package communities owns communities, memberships, rules, themes, community domains and discovery.
package communities

import (
	"context"
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
	return c, g, nil
}
