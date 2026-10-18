package federation

import (
	"bytes"
	"crypto/rand"
	"encoding/base32"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"
)

// activity is the envelope of an incoming activity. Object stays raw, because
// it is a URL or an embedded object depending on the type and the peer.
type activity struct {
	ID     string          `json:"id"`
	Type   string          `json:"type"`
	Actor  ref             `json:"actor"`
	Object json.RawMessage `json:"object"`
	// Summary and Content carry a Flag's reason: Lemmy writes the first and
	// Mastodon the second.
	Summary string `json:"summary,omitempty"`
	Content string `json:"content,omitempty"`
}

// ref is a link that a peer writes as a URL string or as an object with an id.
type ref struct {
	ID string
}

func (r *ref) UnmarshalJSON(b []byte) error {
	b = bytes.TrimSpace(b)
	if len(b) > 0 && b[0] == '"' {
		return json.Unmarshal(b, &r.ID)
	}
	var obj struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(b, &obj); err != nil {
		return err
	}
	r.ID = obj.ID
	return nil
}

func (r ref) MarshalJSON() ([]byte, error) { return json.Marshal(r.ID) }

// objectRef is the id of the object, whether linked or embedded.
func (a activity) objectRef() string {
	var r ref
	if len(a.Object) == 0 || json.Unmarshal(a.Object, &r) != nil {
		return ""
	}
	return r.ID
}

// embedded decodes an embedded object activity; it fails for a bare link.
func (a activity) embedded() (activity, error) {
	var inner activity
	if len(a.Object) == 0 || bytes.TrimSpace(a.Object)[0] != '{' {
		return inner, errors.New("object is not embedded")
	}
	err := json.Unmarshal(a.Object, &inner)
	return inner, err
}

// checkHosts rejects any object whose host differs from its author's host,
// at every level of embedding, and an Announce of an activity that does not
// address the announcing group.
func checkHosts(body []byte) error {
	var doc map[string]any
	if err := json.Unmarshal(body, &doc); err != nil {
		return fmt.Errorf("body is not JSON: %w", err)
	}
	if err := checkObjectHosts(doc, 0); err != nil {
		return err
	}
	if doc["type"] == "Announce" {
		inner, ok := doc["object"].(map[string]any)
		if !ok {
			return nil
		}
		group := linkID(doc["actor"])
		if !addresses(inner, group) {
			return fmt.Errorf("announced activity %s does not belong to group %s", linkID(inner["id"]), group)
		}
	}
	return nil
}

func checkObjectHosts(obj map[string]any, depth int) error {
	if depth > 4 {
		return errors.New("objects nest too deeply")
	}
	id := linkID(obj["id"])
	author := linkID(obj["attributedTo"])
	if author == "" {
		author = linkID(obj["actor"])
	}
	if id != "" && author != "" && !sameHost(id, author) {
		return fmt.Errorf("object %s is not on the host of its author %s", id, author)
	}
	if inner, ok := obj["object"].(map[string]any); ok {
		if err := checkObjectHosts(inner, depth+1); err != nil {
			return err
		}
		// A created or updated object is authored by the actor that sends it.
		t, _ := obj["type"].(string)
		if (t == "Create" || t == "Update") && linkID(inner["attributedTo"]) != "" && !sameHost(linkID(inner["attributedTo"]), author) {
			return fmt.Errorf("%s by %s carries an object by %s", t, author, linkID(inner["attributedTo"]))
		}
	}
	return nil
}

// linkID reads a link as a string, an object with an id, or the first entry of a list.
func linkID(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case map[string]any:
		s, _ := x["id"].(string)
		return s
	case []any:
		for _, e := range x {
			if s := linkID(e); s != "" {
				return s
			}
		}
	}
	return ""
}

// addresses reports whether an activity names target in to, cc, audience or target.
func addresses(obj map[string]any, target string) bool {
	if target == "" {
		return false
	}
	for _, field := range []string{"to", "cc", "audience", "target"} {
		switch x := obj[field].(type) {
		case string:
			if x == target {
				return true
			}
		case []any:
			for _, e := range x {
				if linkID(e) == target {
					return true
				}
			}
		case map[string]any:
			if linkID(x) == target {
				return true
			}
		}
	}
	return false
}

func sameHost(a, b string) bool {
	ua, err1 := url.Parse(a)
	ub, err2 := url.Parse(b)
	return err1 == nil && err2 == nil && ua.Host != "" && strings.EqualFold(ua.Host, ub.Host)
}

// newActivityID is a fresh AP ID for an activity sent by actor, on actor's host.
func newActivityID(actorApID string) (string, error) {
	u, err := url.Parse(actorApID)
	if err != nil || u.Host == "" {
		return "", fmt.Errorf("actor AP ID %q has no host", actorApID)
	}
	b := make([]byte, 15)
	_, _ = rand.Read(b)
	return u.Scheme + "://" + u.Host + "/a/" + strings.ToLower(base32.StdEncoding.EncodeToString(b)), nil
}
