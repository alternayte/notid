package federation

import "time"

// activityStreams is the JSON-LD context every actor document needs.
var activityStreams = []string{
	"https://www.w3.org/ns/activitystreams",
	"https://w3id.org/security/v1",
}

const (
	typePerson      = "Person"
	typeGroup       = "Group"
	typeService     = "Service"
	typeApplication = "Application"
)

// endpoints is the actor's shared inbox, which lets a sender deliver one
// activity for many recipients.
type endpoints struct {
	SharedInbox string `json:"sharedInbox"`
}

type publicKey struct {
	ID           string `json:"id"`
	Owner        string `json:"owner"`
	PublicKeyPem string `json:"publicKeyPem"`
}

// actorDoc is a Person or Group document.
type actorDoc struct {
	Context           []string   `json:"@context"`
	ID                string     `json:"id"`
	Type              string     `json:"type"`
	PreferredUsername string     `json:"preferredUsername"`
	Name              string     `json:"name,omitempty"`
	Summary           string     `json:"summary,omitempty"`
	URL               string     `json:"url,omitempty"`
	Inbox             string     `json:"inbox"`
	Outbox            string     `json:"outbox"`
	Followers         string     `json:"followers"`
	Following         string     `json:"following,omitempty"`
	Endpoints         *endpoints `json:"endpoints,omitempty"`
	PublicKey         publicKey  `json:"publicKey"`
	Published         *time.Time `json:"published,omitempty"`
}

func personDoc(a actorRow, url string) actorDoc {
	kind := typePerson
	if a.Bot {
		kind = typeService
	}
	return actorDoc{
		Context:           activityStreams,
		ID:                a.ApID,
		Type:              kind,
		PreferredUsername: a.Name,
		Name:              a.DisplayName,
		Summary:           a.Summary,
		URL:               url,
		Inbox:             a.Inbox,
		Outbox:            a.ApID + "/outbox",
		Followers:         a.ApID + "/followers",
		Following:         a.ApID + "/following",
		Endpoints:         &endpoints{SharedInbox: a.SharedInbox},
		PublicKey:         publicKey{ID: a.ApID + "#main-key", Owner: a.ApID, PublicKeyPem: a.PublicKeyPem},
	}
}

func groupDoc(c groupRow, url string) actorDoc {
	return actorDoc{
		Context:           activityStreams,
		ID:                c.ApID,
		Type:              typeGroup,
		PreferredUsername: c.Name,
		Name:              c.Title,
		Summary:           c.Description,
		URL:               url,
		Inbox:             c.Inbox,
		Outbox:            c.ApID + "/outbox",
		Followers:         c.ApID + "/followers",
		Endpoints:         &endpoints{SharedInbox: c.Inbox},
		PublicKey:         publicKey{ID: c.ApID + "#main-key", Owner: c.ApID, PublicKeyPem: c.PublicKeyPem},
	}
}

type actorRow struct {
	ApID         string
	Name         string
	DisplayName  string
	Summary      string
	Inbox        string
	SharedInbox  string
	PublicKeyPem string
	Bot          bool
}

type groupRow struct {
	ApID         string
	Name         string
	Title        string
	Description  string
	Inbox        string
	PublicKeyPem string
}
