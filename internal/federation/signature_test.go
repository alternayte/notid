package federation

import (
	"bytes"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func testKey(t *testing.T) (*rsa.PrivateKey, string) {
	t.Helper()
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	der, err := x509.MarshalPKIXPublicKey(&key.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return key, string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: der}))
}

// signedPost builds the request as a peer sends it and as the inbox receives it.
func signedPost(t *testing.T, key *rsa.PrivateKey, body []byte, at time.Time) *http.Request {
	t.Helper()
	out, err := http.NewRequest(http.MethodPost, "https://notid.test/inbox", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	out.Header.Set("Content-Type", "application/activity+json")
	if err := signRequest(out, "https://peer.test/u/bob#main-key", key, body, at); err != nil {
		t.Fatal(err)
	}
	in := httptest.NewRequest(http.MethodPost, "/inbox", bytes.NewReader(body))
	in.Host = "notid.test"
	in.Header = out.Header.Clone()
	return in
}

func verify(r *http.Request, body []byte, pub string, now time.Time) error {
	p, err := parseSignature(r)
	if err != nil {
		return err
	}
	if err := checkSignedRequest(r, p, body, now, time.Hour); err != nil {
		return err
	}
	return verifyWithKey(r, p, pub)
}

func TestSignedPostVerifies(t *testing.T) {
	key, pub := testKey(t)
	now := time.Now()
	body := []byte(`{"type":"Follow"}`)
	r := signedPost(t, key, body, now)
	if err := verify(r, body, pub, now); err != nil {
		t.Fatal(err)
	}
	p, _ := parseSignature(r)
	if keyOwner(p.KeyID) != "https://peer.test/u/bob" {
		t.Fatalf("key owner: %s", keyOwner(p.KeyID))
	}
}

func TestSignatureRejectsATamperedBodyAnotherKeyAndAnOldDate(t *testing.T) {
	key, pub := testKey(t)
	_, otherPub := testKey(t)
	now := time.Now()
	body := []byte(`{"type":"Follow"}`)

	if err := verify(signedPost(t, key, body, now), []byte(`{"type":"Delete"}`), pub, now); err == nil {
		t.Error("tampered body verified")
	}
	if err := verify(signedPost(t, key, body, now), body, otherPub, now); err == nil {
		t.Error("another key verified")
	}
	if err := verify(signedPost(t, key, body, now.Add(-2*time.Hour)), body, pub, now); err == nil {
		t.Error("old date verified")
	}
	r := signedPost(t, key, body, now)
	r.Header.Set("Signature", strings.Replace(r.Header.Get("Signature"), " digest content-type", "", 1))
	if err := verify(r, body, pub, now); err == nil || !strings.Contains(err.Error(), "digest") {
		t.Errorf("signature without digest: %v", err)
	}
}

func TestSignatureAcceptsHS2019AndAuthorizationHeader(t *testing.T) {
	key, pub := testKey(t)
	now := time.Now()
	body := []byte(`{}`)
	r := signedPost(t, key, body, now)
	sig := strings.Replace(r.Header.Get("Signature"), "rsa-sha256", "hs2019", 1)
	r.Header.Del("Signature")
	r.Header.Set("Authorization", "Signature "+sig)
	if err := verify(r, body, pub, now); err != nil {
		t.Fatal(err)
	}
}
