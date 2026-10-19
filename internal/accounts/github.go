package accounts

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/alternayte/auth-all/apierr"
	"github.com/alternayte/auth-all/hook"
	"github.com/alternayte/auth-all/ratelimit"
	"github.com/danielgtaylor/huma/v2"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts/accountsdb"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
)

// GitHub sign-in: auth-all runs the OAuth flow, and notid decides who becomes a
// user. A new user needs a sign-up started in this browser, sealed in a cookie
// that only the OAuth callback receives. The before-create hook checks it; the
// after-create hook makes the person actor.

const (
	signUpCookie  = "notid_github_signup"
	signUpPurpose = "github-signup"
	signUpTTL     = 15 * time.Minute
)

// The codes reach /login as ?error=CODE.
var (
	errAccountRequired = apierr.New("ACCOUNT_REQUIRED", http.StatusForbidden, "No account uses this GitHub login. Create one first.")
	errGitHubClosed    = apierr.New("SIGNUP_CLOSED", http.StatusForbidden, "Sign-up is closed on this site.")
	errGitHubInvite    = apierr.New("INVITE_INVALID", http.StatusForbidden, "The invite code is not valid or was already used.")
	errNameRequired    = apierr.New("NAME_REQUIRED", http.StatusConflict, "The GitHub username is taken or is not a valid name here.")
	errGitHubNameTaken = apierr.New("NAME_TAKEN", http.StatusConflict, "That name is taken.")
)

// pendingSignUp is what the sign-up cookie carries.
type pendingSignUp struct {
	Name    string    `json:"name,omitempty"`
	Invite  string    `json:"invite,omitempty"`
	Expires time.Time `json:"expires"`
	// name is the checked name that the after-create hook gives the person.
	name string
}

type pendingKey struct{}

type gitHubStartBody struct {
	SignUp bool   `json:"signUp" doc:"Create a user when this GitHub login has none"`
	Name   string `json:"name,omitempty" pattern:"^[a-z][a-z0-9_]{2,19}$" doc:"A name to use instead of the GitHub username"`
	Invite string `json:"invite,omitempty" maxLength:"64" doc:"Required for sign-up when sign-up is invite-only"`
}

type GitHubStart struct {
	URL string `json:"url" doc:"Send the browser here"`
}

func (s *Service) registerGitHub(api huma.API) {
	huma.Register(api, huma.Operation{
		OperationID: "gitHubStart",
		Method:      http.MethodPost,
		Path:        "/api/github",
		Summary:     "Start a GitHub sign-in, or a sign-up with signUp",
		Tags:        []string{"accounts"},
		Errors:      []int{http.StatusForbidden, http.StatusNotFound, http.StatusConflict, http.StatusUnprocessableEntity, http.StatusTooManyRequests},
	}, func(ctx context.Context, in *struct{ Body gitHubStartBody }) (*struct {
		SetCookie http.Cookie `header:"Set-Cookie"`
		Body      GitHubStart
	}, error) {
		if !s.GitHub {
			return nil, huma.Error404NotFound("GitHub sign-in is not set up on this site.")
		}
		cookie, err := s.startGitHub(ctx, in.Body)
		switch {
		case err == nil:
		case errors.Is(err, ErrSignupClosed):
			return nil, huma.Error403Forbidden("Sign-up is closed on this site.")
		case errors.Is(err, ErrInviteInvalid):
			return nil, huma.Error403Forbidden("That invite code is not valid or was already used.")
		case errors.Is(err, actors.ErrNameTaken):
			return nil, huma.Error409Conflict("That name is taken. Choose another.")
		case errors.Is(err, ErrRateLimited):
			return nil, huma.Error429TooManyRequests("Too many attempts. Try again later.")
		default:
			return nil, err
		}
		return &struct {
			SetCookie http.Cookie `header:"Set-Cookie"`
			Body      GitHubStart
		}{cookie, GitHubStart{URL: s.Auth.BasePath() + "/oauth/github?redirect_to=/"}}, nil
	})
}

// startGitHub checks a sign-up and seals it into the cookie. A sign-in gets a
// cookie that clears an earlier sign-up, so it never creates a user.
func (s *Service) startGitHub(ctx context.Context, in gitHubStartBody) (http.Cookie, error) {
	cookie := http.Cookie{
		Name:     signUpCookie,
		Path:     s.Auth.BasePath() + "/oauth/github/callback",
		HttpOnly: true,
		Secure:   strings.HasPrefix(s.Local.AppOrigin, "https:"),
		SameSite: http.SameSiteLaxMode,
		MaxAge:   -1,
	}
	if !in.SignUp {
		return cookie, nil
	}
	if s.Mode == platform.SignupClosed {
		return cookie, ErrSignupClosed
	}
	if ok, err := s.Limiter.Allow(ctx, ratelimit.Key{Operation: ratelimit.OpSignUp, IP: platform.ClientIP(ctx)}); err != nil {
		return cookie, fmt.Errorf("rate limit: %w", err)
	} else if !ok {
		return cookie, ErrRateLimited
	}
	if s.Mode == platform.SignupInvite {
		if ok, err := s.inviteUnused(ctx, in.Invite); err != nil {
			return cookie, err
		} else if !ok {
			return cookie, ErrInviteInvalid
		}
	}
	if in.Name != "" {
		if taken, err := s.Local.NameTaken(ctx, s.Pool, in.Name); err != nil {
			return cookie, err
		} else if taken {
			return cookie, actors.ErrNameTaken
		}
	}
	p := pendingSignUp{Name: in.Name, Invite: in.Invite, Expires: time.Now().Add(signUpTTL)}
	plain, err := json.Marshal(p)
	if err != nil {
		return cookie, fmt.Errorf("encode sign-up: %w", err)
	}
	cookie.Value = base64.RawURLEncoding.EncodeToString(s.Local.Sealer.Seal(plain, signUpPurpose))
	cookie.MaxAge = int(signUpTTL / time.Second)
	return cookie, nil
}

func (s *Service) inviteUnused(ctx context.Context, code string) (bool, error) {
	if code == "" {
		return false, nil
	}
	_, err := accountsdb.New(s.Pool).LockUnusedInvite(ctx, HashInvite(code))
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("check invite: %w", err)
	}
	return true, nil
}

// GitHubCallback wraps auth-all's handler. On the GitHub callback it opens the
// sign-up cookie into the context for the hooks, and clears the cookie.
func (s *Service) GitHubCallback(next http.Handler) http.Handler {
	path := s.Auth.BasePath() + "/oauth/github/callback"
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != path {
			next.ServeHTTP(w, r)
			return
		}
		if c, err := r.Cookie(signUpCookie); err == nil {
			if p, ok := s.openSignUp(c.Value); ok {
				r = r.WithContext(context.WithValue(r.Context(), pendingKey{}, p))
			}
		}
		http.SetCookie(w, &http.Cookie{
			Name: signUpCookie, Path: path, HttpOnly: true, MaxAge: -1,
			Secure: strings.HasPrefix(s.Local.AppOrigin, "https:"), SameSite: http.SameSiteLaxMode,
		})
		next.ServeHTTP(w, r)
	})
}

func (s *Service) openSignUp(value string) (*pendingSignUp, bool) {
	sealed, err := base64.RawURLEncoding.DecodeString(value)
	if err != nil {
		return nil, false
	}
	plain, err := s.Local.Sealer.Open(sealed, signUpPurpose)
	if err != nil {
		return nil, false
	}
	var p pendingSignUp
	if json.Unmarshal(plain, &p) != nil || time.Now().After(p.Expires) {
		return nil, false
	}
	return &p, true
}

// HookGitHub adds the hooks that let a GitHub sign-in create a user.
func (s *Service) HookGitHub() {
	s.Auth.Hooks().OnBeforeUserCreate(s.beforeGitHubUser)
	s.Auth.Hooks().OnAfterUserCreate(s.afterGitHubUser)
}

// beforeGitHubUser refuses a new user without a valid sign-up. It runs in
// auth-all's transaction, so it only reads.
func (s *Service) beforeGitHubUser(ctx context.Context, ev *hook.UserCreate) error {
	if ev.Provider == "" {
		return nil
	}
	p, _ := ctx.Value(pendingKey{}).(*pendingSignUp)
	if p == nil {
		return errAccountRequired
	}
	if s.Mode == platform.SignupClosed {
		return errGitHubClosed
	}
	name, typed := p.Name, p.Name != ""
	if !typed && ev.Identity != nil {
		// A GitHub login has letters, digits and hyphens.
		name = strings.ReplaceAll(strings.ToLower(ev.Identity.Username), "-", "_")
	}
	if actors.ValidName(name) != nil {
		return errNameRequired
	}
	taken, err := s.Local.NameTaken(ctx, s.Pool, name)
	if err != nil {
		return err
	}
	if taken && typed {
		return errGitHubNameTaken
	}
	if taken {
		return errNameRequired
	}
	if s.Mode == platform.SignupInvite {
		if ok, err := s.inviteUnused(ctx, p.Invite); err != nil {
			return err
		} else if !ok {
			return errGitHubInvite
		}
	}
	p.name = name
	return nil
}

// afterGitHubUser makes the person actor and uses the invite. auth-all has
// committed the user, so a failure deletes it by hand, as SignUp does.
func (s *Service) afterGitHubUser(ctx context.Context, ev *hook.UserCreate) error {
	p, _ := ctx.Value(pendingKey{}).(*pendingSignUp)
	if ev.Provider == "" || p == nil || p.name == "" {
		return nil
	}
	err := s.finishGitHubUser(ctx, ev, p)
	if err == nil {
		return nil
	}
	if delErr := s.Auth.Store().Users().Delete(context.WithoutCancel(ctx), ev.User.ID); delErr != nil {
		return errors.Join(err, fmt.Errorf("remove half-created user %s: %w", ev.User.ID, delErr))
	}
	return err
}

func (s *Service) finishGitHubUser(ctx context.Context, ev *hook.UserCreate, p *pendingSignUp) error {
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck

	q := accountsdb.New(tx)
	var inviteHash []byte
	if s.Mode == platform.SignupInvite {
		inviteHash = HashInvite(p.Invite)
		if _, err := q.LockUnusedInvite(ctx, inviteHash); err != nil {
			return fmt.Errorf("lock invite: %w", err)
		}
	}
	display := []rune(ev.Identity.DisplayName)
	if len(display) > 60 {
		display = display[:60]
	}
	return s.finishSignUp(ctx, tx, q, ev.User.ID, SignUpInput{Name: p.name, DisplayName: string(display)}, inviteHash)
}
