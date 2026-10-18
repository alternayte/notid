package federation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The files in testdata/peers are activities that Lemmy 0.19, PieFed 1.8 and
// notid sent in the interop harness, recorded from the inbox queue by
// INTEROP_RECORD=1 just interop. Each must pass the host checks and decode to
// what the inbox's planners read.
func TestRecordedPeerActivitiesDecode(t *testing.T) {
	files, err := filepath.Glob("testdata/peers/*.json")
	if err != nil || len(files) == 0 {
		t.Fatalf("no fixtures: %v", err)
	}
	for _, f := range files {
		t.Run(filepath.Base(f), func(t *testing.T) {
			body, err := os.ReadFile(f)
			if err != nil {
				t.Fatal(err)
			}
			if err := checkHosts(body); err != nil {
				t.Fatalf("host check: %v", err)
			}
			var act activity
			if err := json.Unmarshal(body, &act); err != nil {
				t.Fatal(err)
			}
			if act.ID == "" || act.Actor.ID == "" || act.objectRef() == "" {
				t.Fatalf("envelope: %+v", act)
			}
			raw := act.Object
			if act.Type == "Announce" {
				if act, err = act.embedded(); err != nil {
					t.Fatalf("announced activity: %v", err)
				}
			}
			switch act.Type {
			case "Create":
				obj, err := decodeObject(act.Object)
				if err != nil {
					t.Fatal(err)
				}
				if obj.AttributedTo.ID != act.Actor.ID {
					t.Fatalf("attributedTo %s, actor %s", obj.AttributedTo.ID, act.Actor.ID)
				}
				if obj.Type == "Note" && obj.InReplyTo == "" {
					t.Fatal("a Note without inReplyTo")
				}
				if obj.Type == "Page" && obj.Name == "" {
					t.Fatal("a Page without a name")
				}
				name := filepath.Base(f)
				if link, isImage := linkOf(obj); strings.Contains(name, "-image-") && (link == "" || !isImage) {
					t.Fatalf("an image post reads as link %q, image %v", link, isImage)
				} else if strings.Contains(name, "-link-") && (link == "" || isImage) {
					t.Fatalf("a link post reads as link %q, image %v", link, isImage)
				}
			case "Page", "Note":
				// A bare announced object, as PieFed and Lemmy send to servers they do not know.
				if _, err := decodeObject(raw); err != nil {
					t.Fatal(err)
				}
			case "Undo", "Accept":
				if _, err := act.embedded(); err != nil {
					t.Fatalf("%s must embed its object: %v", act.Type, err)
				}
			case "Lock", "Block", "Add", "Remove":
				// A moderator acts; only the group's own host vouches for them.
				group := act.community()
				if group == "" || !sameHost(act.Actor.ID, group) {
					t.Fatalf("%s by %s names community %q", act.Type, act.Actor.ID, group)
				}
			case "Update":
				if objectType(act.Object) == "Group" {
					var doc groupObject
					if err := json.Unmarshal(act.Object, &doc); err != nil || !sameHost(doc.ID, act.Actor.ID) || doc.Name == "" {
						t.Fatalf("group update %+v by %s: %v", doc, act.Actor.ID, err)
					}
					break
				}
				obj, err := decodeObject(act.Object)
				if err != nil || obj.ID != act.objectRef() || obj.AttributedTo.ID != act.Actor.ID {
					t.Fatalf("update of %+v by %s: %v", obj, act.Actor.ID, err)
				}
			case "Delete":
				if strings.Contains(filepath.Base(f), "-person") && act.objectRef() != act.Actor.ID {
					t.Fatalf("person delete of %s by %s", act.objectRef(), act.Actor.ID)
				}
			case "Flag":
				if act.Summary == "" && act.Content == "" {
					t.Fatal("a Flag without a reason")
				}
			}
		})
	}
}
