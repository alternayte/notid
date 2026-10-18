package accounts

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts/accountsdb"
)

// Profile is a person as anyone sees them, local or remote.
type Profile struct {
	Handle      string    `json:"handle" doc:"name for people on this site, else name@domain"`
	Name        string    `json:"name"`
	Domain      string    `json:"domain"`
	DisplayName string    `json:"displayName"`
	Bio         string    `json:"bio" doc:"Markdown"`
	BioHTML     string    `json:"bioHtml" doc:"Sanitized HTML from bio"`
	Bot         bool      `json:"bot" doc:"An automated account; its posts carry a label"`
	Local       bool      `json:"local" doc:"true for people who signed up on this site"`
	Joined      time.Time `json:"joined" doc:"When this site first knew the person"`
}

type profileBody struct {
	DisplayName string `json:"displayName" maxLength:"100"`
	Bio         string `json:"bio" maxLength:"2000" doc:"Markdown"`
	Bot         bool   `json:"bot" doc:"Mark the account as automated. Only bot accounts can create API keys"`
}

var errNoPerson = errors.New("no person at that address")

func (s *Service) profile(a accountsdb.Actor) Profile {
	return Profile{
		Handle: s.Local.Handle(a.Name, a.Domain), Name: a.Name, Domain: a.Domain, DisplayName: a.DisplayName,
		Bio: a.Summary, BioHTML: s.Markdown.HTML(a.Summary), Bot: a.Bot, Local: a.Local, Joined: a.CreatedAt,
	}
}

func (s *Service) registerProfiles(api huma.API) {
	tags := []string{"accounts"}
	huma.Register(api, huma.Operation{
		OperationID: "person", Method: http.MethodGet, Path: "/api/people/{handle}",
		Summary: "Read a person's profile", Tags: tags, Errors: []int{http.StatusNotFound},
	}, func(ctx context.Context, in *struct {
		Handle string `path:"handle" maxLength:"300"`
	}) (*struct{ Body Profile }, error) {
		domain, name := s.Local.SplitHandle(in.Handle)
		a, err := accountsdb.New(s.Pool).PersonByHandle(ctx, accountsdb.PersonByHandleParams{Domain: domain, Name: name})
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, huma.Error404NotFound(errNoPerson.Error())
		}
		if err != nil {
			return nil, fmt.Errorf("person %s: %w", in.Handle, err)
		}
		return &struct{ Body Profile }{s.profile(a)}, nil
	})
	huma.Register(api, huma.Operation{
		OperationID: "updateProfile", Method: http.MethodPut, Path: "/api/me/profile",
		Summary: "Change my display name, bio and bot flag", Tags: tags, Errors: []int{http.StatusUnauthorized},
	}, func(ctx context.Context, in *struct{ Body profileBody }) (*struct{ Body Profile }, error) {
		me := ActorFrom(ctx)
		if me == nil {
			return nil, huma.Error401Unauthorized("sign in first")
		}
		a, err := accountsdb.New(s.Pool).UpdateProfile(ctx, accountsdb.UpdateProfileParams{
			ID: me.ID, DisplayName: strings.TrimSpace(in.Body.DisplayName), Summary: in.Body.Bio, Bot: in.Body.Bot,
		})
		if err != nil {
			return nil, fmt.Errorf("update profile: %w", err)
		}
		return &struct{ Body Profile }{s.profile(a)}, nil
	})
}
