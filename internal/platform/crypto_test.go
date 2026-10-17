package platform

import (
	"bytes"
	"testing"
)

func TestSealerRoundTripAndPurposeBinding(t *testing.T) {
	s, err := NewSealer(bytes.Repeat([]byte{7}, 32))
	if err != nil {
		t.Fatal(err)
	}
	sealed := s.Seal([]byte("private key"), "actor-key")
	if bytes.Contains(sealed, []byte("private key")) {
		t.Fatal("plaintext visible in sealed value")
	}
	got, err := s.Open(sealed, "actor-key")
	if err != nil || string(got) != "private key" {
		t.Fatalf("open: %q %v", got, err)
	}
	if _, err := s.Open(sealed, "backup-destination"); err == nil {
		t.Fatal("opened under another purpose")
	}
	other, _ := NewSealer(bytes.Repeat([]byte{8}, 32))
	if _, err := other.Open(sealed, "actor-key"); err == nil {
		t.Fatal("opened with another key")
	}
}
