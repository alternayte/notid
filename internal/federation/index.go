package federation

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"github.com/riverqueue/river/rivertype"

	"github.com/alternayte/notid/internal/federation/federationdb"
)

// IndexArgs reads the community list of one server into the community index.
type IndexArgs struct {
	Server string `json:"server"`
}

func (IndexArgs) Kind() string { return "index" }

// One read of a server at a time: an admin's add and the daily run do not
// both read it, and an add after a finished read reads it again.
func (IndexArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{
		Queue: queueDeliver, MaxAttempts: 3,
		UniqueOpts: river.UniqueOpts{ByArgs: true, ByState: []rivertype.JobState{
			rivertype.JobStateAvailable, rivertype.JobStatePending, rivertype.JobStateRunning,
			rivertype.JobStateRetryable, rivertype.JobStateScheduled,
		}},
	}
}

// IndexAllArgs queues a read of every server the index covers.
type IndexAllArgs struct{}

func (IndexAllArgs) Kind() string { return "index_all" }

func (IndexAllArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queueDeliver, MaxAttempts: 3, UniqueOpts: river.UniqueOpts{ByPeriod: time.Hour}}
}

// daily fires once a day at 04:00 UTC. An interval schedule restarts with the
// process, so a deploy more often than daily would postpone it forever.
type daily struct{}

func (daily) Next(t time.Time) time.Time {
	n := time.Date(t.Year(), t.Month(), t.Day(), 4, 0, 0, 0, time.UTC)
	if !n.After(t) {
		n = n.AddDate(0, 0, 1)
	}
	return n
}

// PeriodicJobs is the daily read of the community index.
func (s *Server) PeriodicJobs() []*river.PeriodicJob {
	return []*river.PeriodicJob{river.NewPeriodicJob(daily{},
		func() (river.JobArgs, *river.InsertOpts) { return IndexAllArgs{}, nil }, nil)}
}

// QueueIndex queues a read of server in tx, for a server an admin just chose.
func (s *Server) QueueIndex(ctx context.Context, tx pgx.Tx, server string) error {
	if _, err := s.River.InsertTx(ctx, tx, IndexArgs{Server: server}, nil); err != nil {
		return fmt.Errorf("queue index of %s: %w", server, err)
	}
	return nil
}

type indexAllWorker struct {
	river.WorkerDefaults[IndexAllArgs]
	s *Server
}

func (w *indexAllWorker) Work(ctx context.Context, _ *river.Job[IndexAllArgs]) error {
	q := federationdb.New(w.s.Pool)
	servers, err := q.IndexServers(ctx)
	if err != nil {
		return fmt.Errorf("index servers: %w", err)
	}
	if err := q.DeleteIndexOutside(ctx, servers); err != nil {
		return fmt.Errorf("drop servers the index left: %w", err)
	}
	for _, server := range servers {
		if _, err := w.s.River.Insert(ctx, IndexArgs{Server: server}, nil); err != nil {
			return fmt.Errorf("queue index of %s: %w", server, err)
		}
	}
	return nil
}

type indexWorker struct {
	river.WorkerDefaults[IndexArgs]
	s *Server
}

// indexAPI is where a server's software lists its local communities, and the
// sort that puts the most active first. PieFed has no monthly sort; its Top
// order comes closest, and the index ranks by monthly active users anyway.
var indexAPI = map[string]struct{ path, sort string }{
	"lemmy":  {"/api/v3/community/list", "TopMonth"},
	"piefed": {"/api/alpha/community/list", "Top"},
}

const (
	indexPageSize    = 50
	indexPages       = 10
	indexDescription = 300
)

func (w *indexWorker) Work(ctx context.Context, job *river.Job[IndexArgs]) error {
	s := w.s
	server := job.Args.Server
	q := federationdb.New(s.Pool)
	peer, err := q.PeerForIndex(ctx, server)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("peer %s: %w", server, err)
	}
	if peer.Blocked {
		return nil
	}
	origin := scheme(s.AppOrigin) + "://" + server
	software, version, err := s.peerSoftware(ctx, origin)
	if err != nil {
		return err
	}
	if err := q.SetPeerSoftware(ctx, federationdb.SetPeerSoftwareParams{Domain: server, Software: software, Version: version}); err != nil {
		return fmt.Errorf("software of %s: %w", server, err)
	}
	api, ok := indexAPI[software]
	if !ok {
		return nil
	}
	var entries []federationdb.InsertIndexEntryParams
	for page := 1; page <= indexPages; page++ {
		list := origin + api.path + "?" + url.Values{
			"type_": {"Local"}, "sort": {api.sort}, "limit": {strconv.Itoa(indexPageSize)}, "page": {strconv.Itoa(page)},
		}.Encode()
		var out struct {
			Communities []listedCommunity `json:"communities"`
		}
		if err := s.getJSON(ctx, list, &out); err != nil {
			return err
		}
		for _, c := range out.Communities {
			if e, ok := c.entry(origin, server, software); ok {
				entries = append(entries, e)
			}
		}
		if len(out.Communities) < indexPageSize {
			break
		}
	}
	// The block check and the rewrite share a transaction, so a block during
	// the read leaves nothing behind.
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	tq := federationdb.New(tx)
	if peer, err := tq.PeerForIndex(ctx, server); err != nil || peer.Blocked {
		return err
	}
	if err := tq.DeleteIndexOfServer(ctx, server); err != nil {
		return fmt.Errorf("clear index of %s: %w", server, err)
	}
	for _, e := range entries {
		if err := tq.InsertIndexEntry(ctx, e); err != nil {
			return fmt.Errorf("index %s: %w", e.ApID, err)
		}
	}
	if err := tq.PeerIndexed(ctx, server); err != nil {
		return fmt.Errorf("indexed %s: %w", server, err)
	}
	return tx.Commit(ctx)
}

// listedCommunity is one community in a Lemmy or PieFed community list.
type listedCommunity struct {
	Community struct {
		ActorID     string `json:"actor_id"`
		Name        string `json:"name"`
		Title       string `json:"title"`
		Description string `json:"description"`
		NSFW        bool   `json:"nsfw"`
		Deleted     bool   `json:"deleted"`
		Removed     bool   `json:"removed"`
		Hidden      bool   `json:"hidden"`
		Visibility  string `json:"visibility"`
	} `json:"community"`
	Counts struct {
		LemmyMonthly  int32 `json:"users_active_month"`
		PieFedMonthly int32 `json:"active_monthly"`
	} `json:"counts"`
}

// entry keeps a community that lives on the server it was listed by and that
// the server shows to everyone.
func (c listedCommunity) entry(origin, server, software string) (federationdb.InsertIndexEntryParams, bool) {
	k := c.Community
	if k.ActorID == "" || k.Name == "" || !sameHost(k.ActorID, origin) || k.Deleted || k.Removed || k.Hidden ||
		(k.Visibility != "" && k.Visibility != "Public") {
		return federationdb.InsertIndexEntryParams{}, false
	}
	return federationdb.InsertIndexEntryParams{
		ApID: k.ActorID, Server: server, Domain: hostOf(k.ActorID), Name: k.Name,
		Title: k.Title, Description: clip(plain(k.Description), indexDescription),
		ActiveMonth: max(c.Counts.LemmyMonthly, c.Counts.PieFedMonthly), Software: software, Nsfw: k.NSFW,
	}, true
}

var (
	mdImage    = regexp.MustCompile(`!\[[^\]]*\]\([^)]*\)`)
	mdLink     = regexp.MustCompile(`\[([^\]]*)\]\([^)]*\)`)
	mdLineMark = regexp.MustCompile(`(?m)^\s*(#{1,6}\s+|>\s?|[-*+]\s+|\d+\.\s+|:::.*$|[-*_]{3,}\s*$)`)
	mdInline   = strings.NewReplacer("**", "", "__", "", "~~", "", "`", "")
)

// plain turns a Markdown description into text for one line in a list.
func plain(md string) string {
	s := mdImage.ReplaceAllString(md, "")
	s = mdLink.ReplaceAllString(s, "$1")
	s = mdLineMark.ReplaceAllString(s, "")
	return mdInline.Replace(s)
}

// clip collapses whitespace and cuts s to n characters.
func clip(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= n {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:n-1])) + "…"
}

// peerSoftware reads the software name and version a server publishes.
func (s *Server) peerSoftware(ctx context.Context, origin string) (software, version string, err error) {
	var links struct {
		Links []struct {
			Rel  string `json:"rel"`
			Href string `json:"href"`
		} `json:"links"`
	}
	if err := s.getJSON(ctx, origin+"/.well-known/nodeinfo", &links); err != nil {
		return "", "", err
	}
	href := ""
	for _, l := range links.Links {
		if strings.HasPrefix(l.Rel, "http://nodeinfo.diaspora.software/ns/schema/2.") && sameHost(l.Href, origin) {
			href = l.Href
		}
	}
	if href == "" {
		return "", "", river.JobCancel(fmt.Errorf("%s publishes no nodeinfo 2.x on its own host", origin))
	}
	var doc struct {
		Software struct {
			Name    string `json:"name"`
			Version string `json:"version"`
		} `json:"software"`
	}
	if err := s.getJSON(ctx, href, &doc); err != nil {
		return "", "", err
	}
	return strings.ToLower(doc.Software.Name), doc.Software.Version, nil
}
