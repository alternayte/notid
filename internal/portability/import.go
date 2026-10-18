package portability

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"

	"filippo.io/age"
	"github.com/jackc/pgx/v5"

	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/discussions"
	"github.com/alternayte/notid/internal/portability/portabilitydb"
	"github.com/alternayte/notid/migrations"
)

// maxExportBytes bounds the decrypted archive, so a hostile file cannot fill memory.
const maxExportBytes = 2 << 30

// Imported is the community an import created.
type Imported struct {
	ApID    string
	Domain  string
	Members int
	Posts   int
}

// Import decrypts an export with identity, the owners' age private key, and
// creates the community with the same AP ID in state arriving. ownerEmail
// names a local user who becomes its owner here. Members from the export
// become remote members; the probe makes the community active once DNS
// points at this installation.
func (s *Service) Import(ctx context.Context, r io.Reader, identity, ownerEmail string) (Imported, error) {
	id, err := age.ParseX25519Identity(identity)
	if err != nil {
		return Imported{}, fmt.Errorf("backup key: %w", err)
	}
	files, err := readExport(r, id)
	if err != nil {
		return Imported{}, err
	}
	var man manifest
	if err := json.Unmarshal(files["manifest.json"], &man); err != nil {
		return Imported{}, fmt.Errorf("manifest: %w", err)
	}
	schema, err := migrations.Version()
	if err != nil {
		return Imported{}, err
	}
	if man.Format != "notid-export" || man.FormatVersion > FormatVersion || man.SchemaVersion > schema {
		return Imported{}, fmt.Errorf("export format %q v%d schema %d is newer than this installation (v%d, schema %d)",
			man.Format, man.FormatVersion, man.SchemaVersion, FormatVersion, schema)
	}
	var c communityFile
	if err := json.Unmarshal(files["community.json"], &c); err != nil {
		return Imported{}, fmt.Errorf("community: %w", err)
	}
	if len(files["key.pem"]) == 0 {
		return Imported{}, errors.New("export has no group key")
	}

	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return Imported{}, fmt.Errorf("begin: %w", err)
	}
	defer tx.Rollback(ctx) //nolint:errcheck
	q := portabilitydb.New(tx)
	owner, err := q.ActorIDByEmail(ctx, ownerEmail)
	if errors.Is(err, pgx.ErrNoRows) {
		return Imported{}, fmt.Errorf("no user with email %s here; sign up first", ownerEmail)
	}
	if err != nil {
		return Imported{}, fmt.Errorf("owner: %w", err)
	}
	groupID, err := q.InsertImportedGroup(ctx, portabilitydb.InsertImportedGroupParams{
		ApID: man.Community, Domain: c.Domain, Name: c.Name, DisplayName: c.Title, Summary: c.Description,
		PublicKeyPem: c.PublicKeyPem, PrivateKeyEnc: s.Local.Sealer.Seal(files["key.pem"], actors.KeyPurpose),
	})
	if err != nil {
		return Imported{}, fmt.Errorf("group: %w", err)
	}
	theme := c.Theme
	if len(theme) == 0 {
		theme = []byte("{}")
	}
	communityID, err := q.InsertImportedCommunity(ctx, portabilitydb.InsertImportedCommunityParams{
		ActorID: groupID, Domain: c.Domain, Name: c.Name, Title: c.Title, Description: c.Description,
		Theme: theme, AppealsContact: c.AppealsContact, Nsfw: c.NSFW,
	})
	if err != nil {
		return Imported{}, fmt.Errorf("community: %w", err)
	}
	if err := q.InsertImportedDomain(ctx, portabilitydb.InsertImportedDomainParams{
		Domain: c.Domain, CreatedBy: owner, CommunityID: &communityID,
	}); err != nil {
		return Imported{}, fmt.Errorf("community domain: %w", err)
	}

	out := Imported{ApID: man.Community, Domain: c.Domain}
	stub := func(a authorRef, kind, inbox, shared string) (int64, error) {
		row, err := q.UpsertStubActor(ctx, portabilitydb.UpsertStubActorParams{
			ApID: a.ApID, Kind: kind, Domain: a.Domain, Name: a.Name, Inbox: inbox, SharedInbox: shared,
		})
		return row.ID, err
	}
	if err := eachLine(files["members.jsonl"], func(m memberLine) error {
		actorID, err := stub(authorRef{m.ApID, m.Domain, m.Name}, m.Kind, m.Inbox, m.SharedInbox)
		if err != nil {
			return fmt.Errorf("member %s: %w", m.ApID, err)
		}
		out.Members++
		return q.ImportMembership(ctx, portabilitydb.ImportMembershipParams{
			CommunityID: communityID, ActorID: actorID, Role: m.Role, FollowApID: m.FollowApID,
		})
	}); err != nil {
		return Imported{}, err
	}
	if err := q.ImportMembership(ctx, portabilitydb.ImportMembershipParams{CommunityID: communityID, ActorID: owner, Role: "owner"}); err != nil {
		return Imported{}, fmt.Errorf("owner membership: %w", err)
	}
	if err := eachLine(files["posts.jsonl"], func(p postLine) error {
		authorID, err := stub(p.Author, "person", "", "")
		if err != nil {
			return fmt.Errorf("author %s: %w", p.Author.ApID, err)
		}
		out.Posts++
		_, err = q.ImportPost(ctx, portabilitydb.ImportPostParams{
			ApID: p.ApID, Ref: discussions.Ref(p.ApID), Local: false, CommunityID: communityID, AuthorID: authorID,
			Title: p.Title, Body: p.Body, Url: p.URL, Nsfw: p.NSFW, Published: p.Published, Updated: p.Updated,
			Score: p.Score, Up: p.Up, Down: p.Down,
		})
		return err
	}); err != nil {
		return Imported{}, fmt.Errorf("posts: %w", err)
	}
	if err := eachLine(files["comments.jsonl"], func(m commentLine) error {
		authorID, err := stub(m.Author, "person", "", "")
		if err != nil {
			return fmt.Errorf("author %s: %w", m.Author.ApID, err)
		}
		postID, err := q.PostIDByApID(ctx, m.Post)
		if err != nil {
			return fmt.Errorf("post of %s: %w", m.ApID, err)
		}
		var parentID *int64
		if m.Parent != nil {
			pid, err := q.CommentIDByApID(ctx, *m.Parent)
			if err != nil {
				return fmt.Errorf("parent of %s: %w", m.ApID, err)
			}
			parentID = &pid
		}
		return q.ImportComment(ctx, portabilitydb.ImportCommentParams{
			ApID: m.ApID, Ref: discussions.Ref(m.ApID), Local: false, PostID: postID, ParentID: parentID,
			AuthorID: authorID, Body: m.Body, Published: m.Published, Updated: m.Updated,
			Score: m.Score, Up: m.Up, Down: m.Down,
		})
	}); err != nil {
		return Imported{}, fmt.Errorf("comments: %w", err)
	}
	if err := tx.Commit(ctx); err != nil {
		return Imported{}, fmt.Errorf("commit: %w", err)
	}
	return out, nil
}

// readExport decrypts and unpacks an export into its files.
func readExport(r io.Reader, id age.Identity) (map[string][]byte, error) {
	dec, err := age.Decrypt(r, id)
	if err != nil {
		return nil, fmt.Errorf("decrypt export: %w", err)
	}
	gz, err := gzip.NewReader(io.LimitReader(dec, maxExportBytes))
	if err != nil {
		return nil, fmt.Errorf("export is not gzip: %w", err)
	}
	tr := tar.NewReader(gz)
	files := map[string][]byte{}
	for {
		h, err := tr.Next()
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return nil, fmt.Errorf("read export: %w", err)
		}
		b, err := io.ReadAll(tr)
		if err != nil {
			return nil, fmt.Errorf("read %s: %w", h.Name, err)
		}
		files[h.Name] = b
	}
	for _, name := range []string{"manifest.json", "community.json", "key.pem"} {
		if _, ok := files[name]; !ok {
			return nil, fmt.Errorf("export lacks %s", name)
		}
	}
	return files, nil
}

func eachLine[T any](data []byte, fn func(T) error) error {
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64<<10), 16<<20)
	for sc.Scan() {
		if len(bytes.TrimSpace(sc.Bytes())) == 0 {
			continue
		}
		var v T
		if err := json.Unmarshal(sc.Bytes(), &v); err != nil {
			return fmt.Errorf("decode line: %w", err)
		}
		if err := fn(v); err != nil {
			return err
		}
	}
	return sc.Err()
}
