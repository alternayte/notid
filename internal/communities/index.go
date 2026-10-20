package communities

import (
	"context"
	"fmt"
	"net/http"
	"strings"

	"github.com/danielgtaylor/huma/v2"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/communities/communitiesdb"
)

// IndexEntry is a community from the community index: what a Lemmy or PieFed
// server says about one of its communities.
type IndexEntry struct {
	ApID        string `json:"apId"`
	Handle      string `json:"handle" doc:"name@domain"`
	Name        string `json:"name"`
	Domain      string `json:"domain"`
	Title       string `json:"title"`
	Description string `json:"description" doc:"Plain text, cut short"`
	ActiveMonth int32  `json:"activeMonth" doc:"People active in it in the last month, as its server counts them"`
	Software    string `json:"software" enum:"lemmy,piefed"`
	NSFW        bool   `json:"nsfw"`
	Known       bool   `json:"known,omitempty" doc:"This site has fetched it, so its page opens without a lookup"`
	InDirectory bool   `json:"inDirectory,omitempty" doc:"In the directory; admin listing only"`
}

type indexOut struct {
	Communities []IndexEntry `json:"communities" nullable:"false"`
}

func (s *Service) registerIndex(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "searchIndex", Method: http.MethodGet, Path: "/api/search/index",
		Summary: "Search communities on other servers that nobody here follows, most active first",
		Tags:    []string{"communities"},
	}, func(ctx context.Context, in *struct {
		Q string `query:"q" minLength:"1" maxLength:"200" doc:"Words to find; quotes match a phrase, a leading - excludes a word"`
	}) (*struct{ Body indexOut }, error) {
		var viewer *int64
		if a := accounts.ActorFrom(ctx); a != nil {
			viewer = &a.ID
		}
		rows, err := communitiesdb.New(s.Pool).SearchIndex(ctx, communitiesdb.SearchIndexParams{ViewerID: viewer, Query: in.Q, Lim: 20})
		if err != nil {
			return nil, fmt.Errorf("search index: %w", err)
		}
		out := indexOut{Communities: make([]IndexEntry, 0, len(rows))}
		for _, r := range rows {
			e := indexEntry(communitiesdb.AdminIndexRow{
				ApID: r.ApID, Domain: r.Domain, Name: r.Name, Title: r.Title, Description: r.Description,
				ActiveMonth: r.ActiveMonth, Software: r.Software, Nsfw: r.Nsfw,
			})
			e.Known = r.Known
			out.Communities = append(out.Communities, e)
		}
		return &struct{ Body indexOut }{out}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "communityIndex", Method: http.MethodGet, Path: "/api/admin/index",
		Summary: "List the community index, most active first, to choose communities for the directory",
		Tags:    []string{"communities"},
		Errors:  []int{http.StatusUnauthorized, http.StatusForbidden},
	}, func(ctx context.Context, in *struct {
		Q      string `query:"q" maxLength:"200" doc:"Words to find; empty lists every community"`
		Offset int32  `query:"offset" minimum:"0" default:"0"`
	}) (*struct{ Body indexOut }, error) {
		a, err := signedIn(ctx)
		if err != nil {
			return nil, err
		}
		out, err := s.AdminIndex(ctx, a, in.Q, in.Offset)
		return &struct{ Body indexOut }{indexOut{Communities: out}}, apiError(err)
	})
}

// AdminIndex lists the community index for an admin, 50 at a time.
func (s *Service) AdminIndex(ctx context.Context, admin actors.Actor, query string, offset int32) ([]IndexEntry, error) {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return nil, err
	}
	rows, err := communitiesdb.New(s.Pool).AdminIndex(ctx, communitiesdb.AdminIndexParams{
		Query: strings.TrimSpace(query), ViewerID: admin.ID, Lim: 50, Off: offset,
	})
	if err != nil {
		return nil, fmt.Errorf("community index: %w", err)
	}
	out := make([]IndexEntry, 0, len(rows))
	for _, r := range rows {
		out = append(out, indexEntry(r))
	}
	return out, nil
}

func indexEntry(r communitiesdb.AdminIndexRow) IndexEntry {
	return IndexEntry{
		ApID: r.ApID, Handle: r.Name + "@" + r.Domain, Name: r.Name, Domain: r.Domain, Title: r.Title,
		Description: r.Description, ActiveMonth: r.ActiveMonth, Software: r.Software, NSFW: r.Nsfw,
		InDirectory: r.InDirectory,
	}
}
