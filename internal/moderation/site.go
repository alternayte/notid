package moderation

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/moderation/moderationdb"
)

// requireAdmin refuses anyone who is not a site admin.
func (s *Service) requireAdmin(ctx context.Context, actor actors.Actor) error {
	if s.IsAdmin == nil {
		return ErrNotAdmin
	}
	ok, err := s.IsAdmin(ctx, actor)
	if err != nil {
		return err
	}
	if !ok {
		return ErrNotAdmin
	}
	return nil
}

// SiteBan stops an actor writing anywhere on this installation until expires.
// The ban carries no community, so it applies in every one.
func (s *Service) SiteBan(ctx context.Context, admin actors.Actor, targetID int64, reason string, expires *time.Time) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.requireAdmin(ctx, admin); err != nil {
			return err
		}
		if targetID == admin.ID {
			return ErrSelfBan
		}
		q := moderationdb.New(tx)
		if _, err := q.InsertBan(ctx, moderationdb.InsertBanParams{ActorID: targetID, ExpiresAt: expires}); err != nil {
			return fmt.Errorf("site ban: %w", err)
		}
		// The person keeps nothing they could act through.
		if err := q.DeleteAllMembershipsForBan(ctx, targetID); err != nil {
			return fmt.Errorf("remove memberships: %w", err)
		}
		return writeLog(ctx, q, nil, admin.ID, ActionSiteBan, &targetID, "", reason, expires)
	})
}

// SiteUnban lifts a site ban.
func (s *Service) SiteUnban(ctx context.Context, admin actors.Actor, targetID int64) error {
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := s.requireAdmin(ctx, admin); err != nil {
			return err
		}
		q := moderationdb.New(tx)
		if err := q.DeleteSiteBan(ctx, targetID); err != nil {
			return fmt.Errorf("lift site ban: %w", err)
		}
		return writeLog(ctx, q, nil, admin.ID, ActionSiteUnban, &targetID, "", "", nil)
	})
}

// SiteBanned reports whether actor is banned anywhere on this installation now.
func SiteBanned(ctx context.Context, db moderationdb.DBTX, actorID int64) (bool, error) {
	_, err := moderationdb.New(db).ActiveBan(ctx, moderationdb.ActiveBanParams{ActorID: actorID})
	if errors.Is(err, pgx.ErrNoRows) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("site ban: %w", err)
	}
	return true, nil
}

// SiteBanView is one active site ban for the admin page.
type SiteBanView struct {
	Person    string     `json:"person" doc:"Handle of the banned person"`
	Reason    string     `json:"reason"`
	CreatedAt time.Time  `json:"createdAt"`
	ExpiresAt *time.Time `json:"expiresAt,omitempty"`
}

// SiteBans lists the active site bans.
func (s *Service) SiteBans(ctx context.Context, admin actors.Actor) ([]SiteBanView, error) {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return nil, err
	}
	rows, err := moderationdb.New(s.Pool).SiteBans(ctx)
	if err != nil {
		return nil, fmt.Errorf("site bans: %w", err)
	}
	out := make([]SiteBanView, 0, len(rows))
	for _, r := range rows {
		out = append(out, SiteBanView{
			Person: s.Local.Handle(r.Name, r.Domain), Reason: r.Reason,
			CreatedAt: r.CreatedAt, ExpiresAt: r.ExpiresAt,
		})
	}
	return out, nil
}

// SitePerson reads the person a site action names.
func (s *Service) SitePerson(ctx context.Context, handle string) (int64, error) {
	domain, name := s.Local.SplitHandle(handle)
	id, err := moderationdb.New(s.Pool).ActorIDByHandle(ctx, moderationdb.ActorIDByHandleParams{Domain: domain, Name: name})
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, ErrNoObject
	}
	if err != nil {
		return 0, fmt.Errorf("person: %w", err)
	}
	return id, nil
}

// UnbannedPath is readable and writable by a banned person: they can leave.
func unbannedRequest(r *http.Request) bool {
	switch r.Method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	}
	return strings.HasPrefix(r.URL.Path, "/api/auth/") || r.URL.Path == "/api/me"
}

// BlockSiteBans refuses writes from a site-banned person. Reads and sign-out
// still work, and so does deleting the account.
func (s *Service) BlockSiteBans(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if unbannedRequest(r) {
			next.ServeHTTP(w, r)
			return
		}
		a := accounts.ActorFrom(r.Context())
		if a == nil {
			next.ServeHTTP(w, r)
			return
		}
		banned, err := SiteBanned(r.Context(), s.Pool, a.ID)
		if err != nil {
			s.Logger.ErrorContext(r.Context(), "site ban check", "actor", a.ID, "err", err)
			http.Error(w, "the ban check failed; try again", http.StatusInternalServerError)
			return
		}
		if banned {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprintf(w, `{"code":"SITE_BANNED","message":%q}`,
				"This site banned your account. You can still read and delete your account.")
			return
		}
		next.ServeHTTP(w, r)
	})
}

// PeerView is one server this installation exchanges activities with.
type PeerView struct {
	Domain       string     `json:"domain"`
	Software     string     `json:"software,omitempty"`
	Version      string     `json:"version,omitempty"`
	Blocked      bool       `json:"blocked"`
	Reason       string     `json:"reason,omitempty" doc:"Why an admin blocked it"`
	LastOKAt     *time.Time `json:"lastOkAt,omitempty"`
	FailingSince *time.Time `json:"failingSince,omitempty" doc:"When the peer last failed; empty while it answers"`
	IndexSource  bool       `json:"indexSource" doc:"An admin chose to index this server's communities"`
	IndexedAt    *time.Time `json:"indexedAt,omitempty" doc:"The last successful read of its community list"`
	Indexed      int64      `json:"indexed" doc:"Communities in the community index from this server"`
}

// Peers lists the servers this installation knows, with their delivery health.
func (s *Service) Peers(ctx context.Context, admin actors.Actor) ([]PeerView, error) {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return nil, err
	}
	rows, err := moderationdb.New(s.Pool).Peers(ctx, s.Tuning.PeerListSize)
	if err != nil {
		return nil, fmt.Errorf("peers: %w", err)
	}
	out := make([]PeerView, 0, len(rows))
	for _, r := range rows {
		out = append(out, PeerView{
			Domain: r.Domain, Software: r.Software, Version: r.Version,
			Blocked: r.Blocked, Reason: r.Reason, LastOKAt: r.LastOkAt, FailingSince: r.FailingSince,
			IndexSource: r.IndexSource, IndexedAt: r.IndexedAt, Indexed: r.Indexed,
		})
	}
	return out, nil
}

// ErrBadDomain refuses a domain this site cannot block.
var ErrBadDomain = errors.New("that is not a domain this site can block")

// SetPeerBlock stops or resumes federation with one server. A blocked peer's
// activities are refused, nothing more is sent to it, and its communities
// leave Discover, search and the community index.
func (s *Service) SetPeerBlock(ctx context.Context, admin actors.Actor, domain string, blocked bool, reason string) error {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return err
	}
	domain = strings.ToLower(strings.TrimSpace(domain))
	if domain == "" || len(domain) > 253 || domain == s.Local.AppDomain || strings.ContainsAny(domain, "/:@ \t") {
		return ErrBadDomain
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		q := moderationdb.New(tx)
		if err := q.SetPeerBlocked(ctx, moderationdb.SetPeerBlockedParams{
			Domain: domain, Blocked: blocked, Reason: reason,
		}); err != nil {
			return fmt.Errorf("block peer %s: %w", domain, err)
		}
		if !blocked {
			return nil
		}
		if err := q.DeleteIndexOfDomain(ctx, domain); err != nil {
			return fmt.Errorf("index of %s: %w", domain, err)
		}
		return nil
	})
}

// SetIndexSource adds a server to the servers the community index reads, and
// queues its first read, or takes it off that list. A server named with a port
// is read on that port.
func (s *Service) SetIndexSource(ctx context.Context, admin actors.Actor, server string, on bool) error {
	if err := s.requireAdmin(ctx, admin); err != nil {
		return err
	}
	server = strings.ToLower(strings.TrimSpace(server))
	host, port, hasPort := strings.Cut(server, ":")
	if host == "" || len(server) > 253 || host == s.Local.AppDomain || strings.ContainsAny(server, "/@ \t") ||
		(hasPort && (port == "" || strings.Trim(port, "0123456789") != "")) {
		return ErrBadDomain
	}
	return s.inTx(ctx, func(tx pgx.Tx) error {
		if err := moderationdb.New(tx).SetPeerIndexSource(ctx, moderationdb.SetPeerIndexSourceParams{
			Domain: server, IndexSource: on,
		}); err != nil {
			return fmt.Errorf("index source %s: %w", server, err)
		}
		if !on || s.QueueIndex == nil {
			return nil
		}
		return s.QueueIndex(ctx, tx, server)
	})
}
