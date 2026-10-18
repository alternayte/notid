//go:build interop

package interop_test

import (
	"fmt"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"filippo.io/age"
)

const (
	moveDomain = "woodwork.club"
	moveGroup  = "http://woodwork.club:8080/c/wood"
)

// pointDNS makes CoreDNS answer the community domain with installation ip.
func pointDNS(t *testing.T, ip string) {
	t.Helper()
	path := filepath.Join(".run", "hosts")
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var lines []string
	for _, line := range strings.Split(string(b), "\n") {
		if strings.HasSuffix(line, " "+moveDomain) {
			line = ip + " " + moveDomain
		}
		lines = append(lines, line)
	}
	if err := os.WriteFile(path, []byte(strings.Join(lines, "\n")), 0o644); err != nil {
		t.Fatal(err)
	}
}

func notidOutput(t *testing.T, service string, args ...string) []byte {
	t.Helper()
	out, err := exec.Command("docker", append([]string{"compose", "-f", "compose.yaml", "exec", "-T", service, "notid"}, args...)...).Output()
	if err != nil {
		t.Fatalf("notid %v on %s: %v", args, service, err)
	}
	return out
}

func copyInto(t *testing.T, service, dst string, data []byte) {
	t.Helper()
	f := filepath.Join(t.TempDir(), filepath.Base(dst))
	if err := os.WriteFile(f, data, 0o644); err != nil {
		t.Fatal(err)
	}
	if out, err := exec.Command("docker", "compose", "-f", "compose.yaml", "cp", f, service+":"+dst).CombinedOutput(); err != nil {
		t.Fatalf("copy into %s: %v\n%s", service, err, out)
	}
}

// slowly polls longer than eventually, to cover a probe round.
func slowly(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(3 * time.Minute)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(3 * time.Second)
	}
	t.Fatalf("timed out: %s", what)
}

func TestPlannedMoveFromAToB(t *testing.T) {
	t.Cleanup(func() { pointDNS(t, "11.11.0.10") })
	if got := notid(t, "notid-a", "community", "create", "wood", "-domain", moveDomain); got != moveGroup {
		t.Fatalf("group %q", got)
	}
	peer := lemmy(t)
	lemmyID := peer.resolveCommunity(moveGroup)
	time.Sleep(8 * time.Second)
	peer.follow(lemmyID)
	eventually(t, "Lemmy follows the community on A", func() bool { return peer.subscribed(lemmyID) == "Subscribed" })

	member := signUp(t, "http://a.notid.test:8080")
	trust(t, "notid-a-db", member)
	member.post("/api/communities/follow", map[string]string{"community": moveGroup}, nil)
	var before struct{ ApID string }
	member.post("/api/posts", map[string]any{"community": moveGroup, "title": "Before the move"}, &before)
	eventually(t, "Lemmy receives the post from before the move", func() bool {
		_, ok := peer.findPost(before.ApID)
		return ok
	})

	owner := signUp(t, "http://b.notid.test:8080")
	key, err := age.GenerateX25519Identity()
	if err != nil {
		t.Fatal(err)
	}
	notid(t, "notid-a", "community", "freeze", moveDomain)
	export := notidOutput(t, "notid-a", "export", moveDomain, "-recipient", key.Recipient().String())
	copyInto(t, "notid-b", "/tmp/export.age", export)
	copyInto(t, "notid-b", "/tmp/key.txt", []byte(key.String()+"\n"))
	notid(t, "notid-b", "import", "/tmp/export.age", "-identity", "/tmp/key.txt", "-owner", owner.name+"@example.org")
	if s := sql(t, "notid-b-db", "SELECT state FROM communities WHERE name = 'wood'"); s != "arriving" {
		t.Fatalf("B state %q before the DNS switch", s)
	}

	pointDNS(t, "11.11.0.11")
	slowly(t, "B's probe makes the community active", func() bool {
		return sql(t, "notid-b-db", "SELECT state FROM communities WHERE name = 'wood'") == "active"
	})
	slowly(t, "A's probe marks the community departed", func() bool {
		return sql(t, "notid-a-db", "SELECT state FROM communities WHERE name = 'wood'") == "departed"
	})
	if k := sql(t, "notid-a-db", fmt.Sprintf("SELECT count(*) FROM actors WHERE ap_id = '%s' AND private_key_enc IS NOT NULL", moveGroup)); k != "0" {
		t.Fatal("A still holds the group's private key")
	}
	if g := sql(t, "notid-b-db", fmt.Sprintf("SELECT count(*) FROM actors WHERE ap_id = '%s' AND local", moveGroup)); g != "1" {
		t.Fatal("B does not act as the group with the same AP ID")
	}

	// A sender with stale DNS reaches A, which answers 503 so the sender retries.
	req, _ := http.NewRequest(http.MethodPost, "http://127.0.0.1:28080/c/wood/inbox", strings.NewReader("{}"))
	req.Host = "woodwork.club:8080"
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusServiceUnavailable {
		t.Fatalf("A answers the group inbox with %d", resp.StatusCode)
	}

	// The member on A keeps posting through A; the Lemmy follower sees it with no new follow.
	var after struct{ ApID string }
	member.post("/api/posts", map[string]any{"community": moveGroup, "title": "After the move"}, &after)
	eventually(t, "B stores the post from A's member", func() bool {
		return sql(t, "notid-b-db", fmt.Sprintf("SELECT count(*) FROM posts WHERE ap_id = '%s'", after.ApID)) == "1"
	})
	eventually(t, "Lemmy receives the post from after the move", func() bool {
		_, ok := peer.findPost(after.ApID)
		return ok
	})
	if peer.subscribed(lemmyID) != "Subscribed" {
		t.Fatal("Lemmy lost its follow")
	}
}
