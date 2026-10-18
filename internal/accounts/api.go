package accounts

import (
	"context"
	"errors"
	"net/http"

	"github.com/danielgtaylor/huma/v2"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
)

type signUpBody struct {
	Email       string `json:"email" format:"email" maxLength:"254"`
	Name        string `json:"name" pattern:"^[a-z][a-z0-9_]{2,19}$" doc:"Public name, also the handle"`
	DisplayName string `json:"displayName,omitempty" maxLength:"60"`
	Invite      string `json:"invite,omitempty" maxLength:"64" doc:"Required when sign-up is invite-only"`
}

type Me struct {
	Name        string `json:"name"`
	Handle      string `json:"handle"`
	DisplayName string `json:"displayName"`
	Bot         bool   `json:"bot"`
	Trust       string `json:"trust" enum:"new,trusted"`
	Admin       bool   `json:"admin" doc:"A site admin sees /admin and can act on any community's local content"`
}

type SignupMode struct {
	Mode string `json:"mode" enum:"open,invite,closed"`
}

// Register adds the account operations to the API.
func (s *Service) Register(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID:   "signUp",
		Method:        http.MethodPost,
		Path:          "/api/signup",
		Summary:       "Create an account, then send a sign-in link with magicLinkSend",
		Tags:          []string{"accounts"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusForbidden, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct{ Body signUpBody }) (*struct{}, error) {
		err := s.SignUp(ctx, SignUpInput{
			Email:       in.Body.Email,
			Name:        in.Body.Name,
			DisplayName: in.Body.DisplayName,
			Invite:      in.Body.Invite,
			ClientIP:    platform.ClientIP(ctx),
		})
		switch {
		case err == nil:
			return nil, nil
		case errors.Is(err, ErrSignupClosed):
			return nil, huma.Error403Forbidden("Sign-up is closed on this site.")
		case errors.Is(err, ErrInviteInvalid):
			return nil, huma.Error403Forbidden("That invite code is not valid or was already used.")
		case errors.Is(err, actors.ErrNameTaken):
			return nil, huma.Error409Conflict("That name is taken. Choose another.")
		case errors.Is(err, actors.ErrNameInvalid):
			return nil, huma.Error422UnprocessableEntity("A name has 3 to 20 characters: lowercase letters, digits and underscores, starting with a letter.")
		case errors.Is(err, ErrEmailInvalid):
			return nil, huma.Error422UnprocessableEntity("That email address is not valid.")
		case errors.Is(err, ErrRateLimited):
			return nil, huma.Error429TooManyRequests("Too many attempts. Try again later.")
		default:
			return nil, err
		}
	})

	huma.Register(api, huma.Operation{
		OperationID: "signupMode",
		Method:      http.MethodGet,
		Path:        "/api/signup",
		Summary:     "Read the sign-up mode of this site",
		Tags:        []string{"accounts"},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body SignupMode }, error) {
		return &struct{ Body SignupMode }{SignupMode{Mode: string(s.Mode)}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID: "me",
		Method:      http.MethodGet,
		Path:        "/api/me",
		Summary:     "Read the signed-in person",
		Tags:        []string{"accounts"},
		Errors:      []int{http.StatusUnauthorized},
	}, func(ctx context.Context, _ *struct{}) (*struct{ Body Me }, error) {
		a := ActorFrom(ctx)
		if a == nil {
			return nil, huma.Error401Unauthorized("sign in first")
		}
		trust := ""
		if a.Trust != nil {
			trust = *a.Trust
		}
		admin, err := s.IsAdmin(ctx, *a)
		if err != nil {
			return nil, err
		}
		return &struct{ Body Me }{Me{
			Name:        a.Name,
			Handle:      s.Local.Handle(a.Name, a.Domain),
			DisplayName: a.DisplayName,
			Bot:         a.Bot,
			Trust:       trust,
			Admin:       admin,
		}}, nil
	})

	huma.Register(api, huma.Operation{
		OperationID:   "deleteMe",
		Method:        http.MethodDelete,
		Path:          "/api/me",
		Summary:       "Delete my account, its posts and its comments",
		Tags:          []string{"accounts"},
		DefaultStatus: http.StatusNoContent,
		Errors:        []int{http.StatusUnauthorized, http.StatusConflict},
	}, func(ctx context.Context, _ *struct{}) (*struct{}, error) {
		a := ActorFrom(ctx)
		if a == nil {
			return nil, huma.Error401Unauthorized("sign in first")
		}
		var sole ErrSoleOwner
		switch err := s.DeleteAccount(ctx, *a); {
		case err == nil:
			return nil, nil
		case errors.As(err, &sole):
			return nil, huma.Error409Conflict(sole.Error())
		case errors.Is(err, ErrNotDeletable):
			return nil, huma.Error409Conflict(err.Error())
		default:
			return nil, err
		}
	})
	s.registerProfiles(api)
}
