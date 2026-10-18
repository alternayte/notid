package moderation

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgtype"

	"github.com/alternayte/notid/internal/moderation/moderationdb"
)

// Counts are the public moderation totals for one window. No names, no reasons.
type Counts struct {
	PostsRemoved     int `json:"postsRemoved"`
	CommentsRemoved  int `json:"commentsRemoved"`
	PostsLocked      int `json:"postsLocked"`
	Bans             int `json:"bans" doc:"Community and site bans"`
	ReportsFiled     int `json:"reportsFiled"`
	ReportsUpheld    int `json:"reportsUpheld"`
	ReportsDismissed int `json:"reportsDismissed"`
}

// Transparency is what the public /about/moderation page counts.
type Transparency struct {
	AllTime Counts `json:"allTime"`
	Recent  Counts `json:"recent" doc:"The transparency window, 30 days"`
}

// PublicCounts totals the moderation log and the reports. Anyone may read it.
func (s *Service) PublicCounts(ctx context.Context) (Transparency, error) {
	q := moderationdb.New(s.Pool)
	window := pgtype.Interval{Microseconds: s.Tuning.TransparencyWindow.Microseconds(), Valid: true}
	rows, err := q.ModerationCounts(ctx, window)
	if err != nil {
		return Transparency{}, fmt.Errorf("moderation counts: %w", err)
	}
	var all, recent Counts
	for _, r := range rows {
		switch r.Action {
		case ActionRemovePost:
			all.PostsRemoved, recent.PostsRemoved = int(r.Total), int(r.Recent)
		case ActionRemoveComment:
			all.CommentsRemoved, recent.CommentsRemoved = int(r.Total), int(r.Recent)
		case ActionLock:
			all.PostsLocked, recent.PostsLocked = int(r.Total), int(r.Recent)
		case ActionBan, ActionSiteBan:
			// A community ban and a site ban count as one kind of action.
			all.Bans += int(r.Total)
			recent.Bans += int(r.Recent)
		}
	}
	reports, err := q.ReportCounts(ctx, window)
	if err != nil {
		return Transparency{}, fmt.Errorf("report counts: %w", err)
	}
	all.ReportsFiled, all.ReportsUpheld, all.ReportsDismissed = int(reports.Filed), int(reports.Upheld), int(reports.Dismissed)
	recent.ReportsFiled, recent.ReportsUpheld, recent.ReportsDismissed =
		int(reports.FiledRecent), int(reports.UpheldRecent), int(reports.DismissedRecent)
	return Transparency{AllTime: all, Recent: recent}, nil
}
