package portability

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"time"

	"filippo.io/age"
	"github.com/google/uuid"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/portability/portabilitydb"
	"github.com/alternayte/notid/migrations"
)

// FormatVersion is the version of the notid-export layout.
const FormatVersion = 1

type manifest struct {
	Format        string    `json:"format"`
	FormatVersion int       `json:"formatVersion"`
	SchemaVersion int64     `json:"schemaVersion"`
	Community     string    `json:"community"`
	Source        uuid.UUID `json:"sourceInstallation"`
	CreatedAt     time.Time `json:"createdAt"`
}

type communityFile struct {
	Domain         string          `json:"domain"`
	Name           string          `json:"name"`
	Title          string          `json:"title"`
	Description    string          `json:"description"`
	Theme          json.RawMessage `json:"theme"`
	AppealsContact string          `json:"appealsContact"`
	NSFW           bool            `json:"nsfw"`
	PublicKeyPem   string          `json:"publicKeyPem"`
}

type memberLine struct {
	ApID        string  `json:"apId"`
	Kind        string  `json:"kind"`
	Domain      string  `json:"domain"`
	Name        string  `json:"name"`
	Inbox       string  `json:"inbox"`
	SharedInbox string  `json:"sharedInbox"`
	Role        string  `json:"role"`
	FollowApID  *string `json:"followApId,omitempty"`
}

type authorRef struct {
	ApID   string `json:"apId"`
	Domain string `json:"domain"`
	Name   string `json:"name"`
}

type postLine struct {
	ApID      string     `json:"apId"`
	Author    authorRef  `json:"author"`
	Title     string     `json:"title"`
	Body      string     `json:"body"`
	URL       string     `json:"url"`
	NSFW      bool       `json:"nsfw"`
	Published time.Time  `json:"published"`
	Updated   *time.Time `json:"updated,omitempty"`
	Score     int32      `json:"score"`
	Up        int32      `json:"up"`
	Down      int32      `json:"down"`
}

type commentLine struct {
	ApID      string     `json:"apId"`
	Author    authorRef  `json:"author"`
	Post      string     `json:"post"`
	Parent    *string    `json:"parent,omitempty"`
	Body      string     `json:"body"`
	Published time.Time  `json:"published"`
	Updated   *time.Time `json:"updated,omitempty"`
	Score     int32      `json:"score"`
	Up        int32      `json:"up"`
	Down      int32      `json:"down"`
}

// Export writes the community on domain as a tar.gz encrypted to recipient,
// an age public key. Nothing leaves the process unencrypted, and the file
// holds no reports and no individual votes.
func (s *Service) Export(ctx context.Context, domain, recipient string, w io.Writer) error {
	r, err := age.ParseX25519Recipient(recipient)
	if err != nil {
		return fmt.Errorf("backup key: %w", err)
	}
	q := portabilitydb.New(s.Pool)
	c, err := q.ExportCommunity(ctx, domain)
	if err != nil {
		return fmt.Errorf("community on %s: %w", domain, err)
	}
	key, err := s.Local.Sealer.Open(c.PrivateKeyEnc, actors.KeyPurpose)
	if err != nil {
		return fmt.Errorf("group key: %w", err)
	}
	self, err := q.InstallationID(ctx)
	if err != nil {
		return fmt.Errorf("installation id: %w", err)
	}
	schema, err := migrations.Version()
	if err != nil {
		return err
	}

	enc, err := age.Encrypt(w, r)
	if err != nil {
		return fmt.Errorf("encrypt: %w", err)
	}
	gz := gzip.NewWriter(enc)
	tw := tar.NewWriter(gz)
	put := func(name string, data []byte) error {
		if err := tw.WriteHeader(&tar.Header{Name: name, Mode: 0o600, Size: int64(len(data)), ModTime: time.Now()}); err != nil {
			return err
		}
		_, err := tw.Write(data)
		return err
	}
	putJSON := func(name string, v any) error {
		b, err := json.MarshalIndent(v, "", "  ")
		if err != nil {
			return err
		}
		return put(name, b)
	}
	putLines := func(name string, n int, line func(i int) any) error {
		var buf []byte
		for i := range n {
			b, err := json.Marshal(line(i))
			if err != nil {
				return err
			}
			buf = append(append(buf, b...), '\n')
		}
		return put(name, buf)
	}

	members, err := q.ExportMembers(ctx, c.ID)
	if err != nil {
		return fmt.Errorf("members: %w", err)
	}
	posts, err := q.ExportPosts(ctx, c.ID)
	if err != nil {
		return fmt.Errorf("posts: %w", err)
	}
	comments, err := q.ExportComments(ctx, c.ID)
	if err != nil {
		return fmt.Errorf("comments: %w", err)
	}
	steps := []func() error{
		func() error {
			return putJSON("manifest.json", manifest{
				Format: "notid-export", FormatVersion: FormatVersion, SchemaVersion: schema,
				Community: c.ApID, Source: self, CreatedAt: time.Now().UTC(),
			})
		},
		func() error {
			return putJSON("community.json", communityFile{
				Domain: c.Domain, Name: c.Name, Title: c.Title, Description: c.Description, Theme: c.Theme,
				AppealsContact: c.AppealsContact, NSFW: c.Nsfw, PublicKeyPem: c.PublicKeyPem,
			})
		},
		func() error {
			return putLines("members.jsonl", len(members), func(i int) any {
				m := members[i]
				return memberLine{ApID: m.ApID, Kind: m.Kind, Domain: m.Domain, Name: m.Name, Inbox: m.Inbox,
					SharedInbox: m.SharedInbox, Role: m.Role, FollowApID: m.FollowApID}
			})
		},
		func() error {
			return putLines("posts.jsonl", len(posts), func(i int) any {
				p := posts[i]
				return postLine{ApID: p.ApID, Author: authorRef{p.AuthorApID, p.AuthorDomain, p.AuthorName},
					Title: p.Title, Body: p.Body, URL: p.Url, NSFW: p.Nsfw, Published: p.Published, Updated: p.Updated,
					Score: p.Score, Up: p.Up, Down: p.Down}
			})
		},
		func() error {
			return putLines("comments.jsonl", len(comments), func(i int) any {
				m := comments[i]
				return commentLine{ApID: m.ApID, Author: authorRef{m.AuthorApID, m.AuthorDomain, m.AuthorName},
					Post: m.PostApID, Parent: m.ParentApID, Body: m.Body, Published: m.Published, Updated: m.Updated,
					Score: m.Score, Up: m.Up, Down: m.Down}
			})
		},
		// The public moderation log arrives with moderation in S5; the file keeps its place.
		func() error { return put("log.jsonl", nil) },
		func() error { return put("key.pem", key) },
	}
	for _, step := range steps {
		if err := step(); err != nil {
			return fmt.Errorf("write export: %w", err)
		}
	}
	if err := tw.Close(); err != nil {
		return fmt.Errorf("close tar: %w", err)
	}
	if err := gz.Close(); err != nil {
		return fmt.Errorf("close gzip: %w", err)
	}
	if err := enc.Close(); err != nil {
		return fmt.Errorf("close age: %w", err)
	}
	return nil
}
