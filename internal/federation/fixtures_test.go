package federation

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

// The files in testdata/peers are activities that Lemmy 0.19, PieFed 1.8 and
// notid sent in the interop harness, recorded from the inbox queue. Each must
// pass the host checks and decode the way the inbox reads it.
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
			case "Page", "Note":
				// A bare announced object, as PieFed and Lemmy send to servers they do not know.
				if _, err := decodeObject(raw); err != nil {
					t.Fatal(err)
				}
			case "Undo", "Accept":
				if _, err := act.embedded(); err != nil {
					t.Fatalf("%s must embed its object: %v", act.Type, err)
				}
			}
		})
	}
}
