package discussions

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"net/url"
	"strings"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
	"github.com/riverqueue/river"
	"golang.org/x/net/html"

	"github.com/alternayte/notid/internal/discussions/discussionsdb"
)

const queuePreviews = "previews"

// PreviewArgs fetches the page a local link post points to.
type PreviewArgs struct {
	PostID int64 `json:"post_id"`
}

func (PreviewArgs) Kind() string { return "link-preview" }

func (PreviewArgs) InsertOpts() river.InsertOpts {
	return river.InsertOpts{Queue: queuePreviews, MaxAttempts: 3}
}

type previewWorker struct {
	river.WorkerDefaults[PreviewArgs]
	s *Service
}

func (w *previewWorker) Work(ctx context.Context, job *river.Job[PreviewArgs]) error {
	return w.s.FetchPreview(ctx, job.Args.PostID)
}

// AddWorkers registers the link preview worker.
func (s *Service) AddWorkers(workers *river.Workers) {
	river.AddWorker(workers, &previewWorker{s: s})
}

// Queues is the River queue config for link previews.
func (s *Service) Queues() map[string]river.QueueConfig {
	return map[string]river.QueueConfig{queuePreviews: {MaxWorkers: s.Tuning.PreviewWorkers}}
}

func (s *Service) queuePreview(ctx context.Context, tx pgx.Tx, p Post) error {
	if p.Url == "" || p.MediaID != nil {
		return nil
	}
	if _, err := s.River.InsertTx(ctx, tx, PreviewArgs{PostID: p.ID}, nil); err != nil {
		return fmt.Errorf("queue preview of post %d: %w", p.ID, err)
	}
	return nil
}

// pagePreview is what a linked page says about itself.
type pagePreview struct {
	isImage                   bool
	title, description, image string
}

// FetchPreview fetches a link post's page through the guarded client and keeps
// its title, description and image on the post. A link to an image shows as one.
// A page that cannot be read gets no preview; only a failed request retries.
func (s *Service) FetchPreview(ctx context.Context, postID int64) error {
	q := discussionsdb.New(s.Pool)
	link, err := q.LinkToPreview(ctx, postID)
	if errors.Is(err, pgx.ErrNoRows) || (err == nil && link == "") {
		return nil
	}
	if err != nil {
		return fmt.Errorf("post %d: %w", postID, err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, link, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("Accept", "text/html, image/*;q=0.8")
	resp, err := s.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("fetch %s: %w", link, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil
	}
	p, err := readPreview(resp, s.Tuning.PreviewReadBytes)
	if err != nil {
		return nil
	}
	return q.SetLinkPreview(ctx, discussionsdb.SetLinkPreviewParams{
		ID: postID, Url: link, UrlImage: p.isImage,
		PreviewTitle: p.title, PreviewDescription: p.description, PreviewImage: p.image,
	})
}

func readPreview(resp *http.Response, limit int64) (pagePreview, error) {
	body := io.LimitReader(resp.Body, limit)
	head := make([]byte, 512)
	n, err := io.ReadFull(body, head)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) {
		return pagePreview{}, err
	}
	sniffed := http.DetectContentType(head[:n])
	if strings.HasPrefix(sniffed, "image/") && sniffed != "image/svg+xml" {
		return pagePreview{isImage: true}, nil
	}
	if mt, _, _ := mime.ParseMediaType(resp.Header.Get("Content-Type")); mt != "text/html" && !strings.HasPrefix(sniffed, "text/html") {
		return pagePreview{}, errors.New("not an HTML page")
	}
	return parsePreview(io.MultiReader(bytes.NewReader(head[:n]), body), resp.Request.URL), nil
}

const (
	previewTitleRunes       = 200
	previewDescriptionRunes = 300
)

// parsePreview reads Open Graph and Twitter card tags from a page's head, and
// falls back to its title and description.
func parsePreview(r io.Reader, base *url.URL) pagePreview {
	var p pagePreview
	var docTitle, metaDescription string
	inTitle := false
	z := html.NewTokenizer(r)
	for {
		switch z.Next() {
		case html.ErrorToken:
			return finishPreview(p, docTitle, metaDescription, base)
		case html.StartTagToken, html.SelfClosingTagToken:
			name, hasAttr := z.TagName()
			switch string(name) {
			case "title":
				inTitle = docTitle == ""
			case "meta":
				if !hasAttr {
					continue
				}
				key, content := metaAttrs(z)
				switch key {
				case "og:title":
					p.title = content
				case "og:description":
					p.description = content
				case "description":
					metaDescription = content
				case "og:image", "og:image:url", "twitter:image":
					if p.image == "" {
						p.image = content
					}
				}
			case "body":
				return finishPreview(p, docTitle, metaDescription, base)
			}
		case html.TextToken:
			if inTitle {
				docTitle += string(z.Text())
			}
		case html.EndTagToken:
			if name, _ := z.TagName(); string(name) == "title" || string(name) == "head" {
				inTitle = false
				if string(name) == "head" {
					return finishPreview(p, docTitle, metaDescription, base)
				}
			}
		}
	}
}

func metaAttrs(z *html.Tokenizer) (key, content string) {
	for {
		k, v, more := z.TagAttr()
		switch string(k) {
		case "property", "name":
			if key == "" {
				key = strings.ToLower(string(v))
			}
		case "content":
			content = string(v)
		}
		if !more {
			return key, content
		}
	}
}

func finishPreview(p pagePreview, docTitle, metaDescription string, base *url.URL) pagePreview {
	if p.title == "" {
		p.title = docTitle
	}
	if p.description == "" {
		p.description = metaDescription
	}
	p.title = clip(strings.Join(strings.Fields(p.title), " "), previewTitleRunes)
	p.description = clip(strings.Join(strings.Fields(p.description), " "), previewDescriptionRunes)
	if p.image != "" {
		u, err := base.Parse(strings.TrimSpace(p.image))
		if err != nil || (u.Scheme != "https" && u.Scheme != "http") {
			p.image = ""
		} else {
			p.image = u.String()
		}
	}
	return p
}

func clip(s string, runes int) string {
	if utf8.RuneCountInString(s) <= runes {
		return s
	}
	r := []rune(s)
	return strings.TrimSpace(string(r[:runes-1])) + "…"
}
