package communities

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
)

// DirectoryView is one remote community an admin reviewed for Discover.
type DirectoryView struct {
	Handle  string    `json:"handle" doc:"name@domain"`
	Title   string    `json:"title"`
	Note    string    `json:"note,omitempty" doc:"Why it is listed; shown to admins"`
	Members int64     `json:"members" doc:"Members known to this installation"`
	AddedAt time.Time `json:"addedAt"`
}

// ErrNotAdmin is returned to anyone but a site admin.
var ErrNotAdmin = errors.New("only a site admin can do that")

func (s *Service) requireAdmin(ctx context.Context, a actors.Actor) error {
	if s.IsAdmin == nil {
		return ErrNotAdmin
	}
	ok, err := s.IsAdmin(ctx, a)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotAdmin
	}
	return nil
}

// Directory lists the reviewed remote communities Discover shows.
func (s *Service) Directory(ctx context.Context, admin actors.Actor) ([]DirectoryView, error) {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return nil, err
	}
	rows, err := communitiesdb.New(s.Pool).Directory(ctx, s.Tuning.DirectorySize)
	if err != nil {
		return nil, fmt.Errorf("directory: %w", err)
	}
	out := make([]DirectoryView, 0, len(rows))
	for _, r := range rows {
		out = append(out, DirectoryView{
			Handle: s.Local.Handle(r.Name, r.Domain), Title: r.Title, Note: r.Note,
			Members: r.Members, AddedAt: r.CreatedAt,
		})
	}
	return out, nil
}

// AddToDirectory lists a remote community in Discover under ref, which is a
// name@domain handle, a URL or an AP ID. The community is fetched and stored
// here the first time it is looked up.
func (s *Service) AddToDirectory(ctx context.Context, admin actors.Actor, ref, note string) error {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return err
	}
	g, err := s.knownGroup(ctx, ref)
	if err != nil || g.Local {
		return fmt.Errorf("%q: %w", ref, ErrNotACommunity)
	}
	c, err := s.communityOf(ctx, communitiesdb.New(s.Pool), g)
	if err != nil {
		return err
	}
	if err := communitiesdb.New(s.Pool).AddToDirectory(ctx, communitiesdb.AddToDirectoryParams{
		CommunityID: c.ID, AddedBy: admin.ID, Note: note,
	}); err != nil {
		return fmt.Errorf("add to directory: %w", err)
	}
	return nil
}

// RemoveFromDirectory takes a community out of Discover.
func (s *Service) RemoveFromDirectory(ctx context.Context, admin actors.Actor, ref string) error {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return err
	}
	g, err := s.knownGroup(ctx, ref)
	if err != nil {
		return fmt.Errorf("%q: %w", ref, ErrNotACommunity)
	}
	c, err := s.communityOf(ctx, communitiesdb.New(s.Pool), g)
	if err != nil {
		return err
	}
	if err := communitiesdb.New(s.Pool).RemoveFromDirectory(ctx, c.ID); err != nil {
		return fmt.Errorf("remove from directory: %w", err)
	}
	return nil
}

// knownGroup finds a group this installation already knows by its stored
// handle, and reaches its peer only when it is new here.
func (s *Service) knownGroup(ctx context.Context, ref string) (actors.Actor, error) {
	g, err := s.storedGroup(ctx, ref)
	if err == nil {
		return g, nil
	}
	return s.Resolve(ctx, s.address(ref))
}

// storedGroup finds a remote group this installation already knows by handle.
func (s *Service) storedGroup(ctx context.Context, ref string) (actors.Actor, error) {
	ref = strings.TrimSpace(ref)
	if strings.Contains(ref, "://") {
		return s.Resolve(ctx, ref)
	}
	domain, name := s.Local.SplitHandle(ref)
	if domain == "" || name == "" || domain == s.Local.AppDomain {
		return actors.Actor{}, ErrNotACommunity
	}
	return actors.GroupByHandle(ctx, s.Pool, domain, name)
}
