package platform

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"errors"
	"fmt"
)

// Sealer encrypts secrets at rest with AES-256-GCM under SECRET_KEY.
type Sealer struct{ aead cipher.AEAD }

func NewSealer(key []byte) (*Sealer, error) {
	if len(key) != 32 {
		return nil, fmt.Errorf("sealer key must be 32 bytes, got %d", len(key))
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, err
	}
	aead, err := cipher.NewGCM(block)
	if err != nil {
		return nil, err
	}
	return &Sealer{aead: aead}, nil
}

// Seal returns nonce || ciphertext. purpose is bound as associated data, so a sealed value cannot be replayed for another purpose.
func (s *Sealer) Seal(plaintext []byte, purpose string) []byte {
	nonce := make([]byte, s.aead.NonceSize(), s.aead.NonceSize()+len(plaintext)+s.aead.Overhead())
	// Since Go 1.24 crypto/rand.Read never returns an error.
	_, _ = rand.Read(nonce)
	return s.aead.Seal(nonce, nonce, plaintext, []byte(purpose))
}

func (s *Sealer) Open(sealed []byte, purpose string) ([]byte, error) {
	n := s.aead.NonceSize()
	if len(sealed) < n+s.aead.Overhead() {
		return nil, errors.New("sealed value is too short")
	}
	out, err := s.aead.Open(nil, sealed[:n], sealed[n:], []byte(purpose))
	if err != nil {
		return nil, fmt.Errorf("open sealed value: %w", err)
	}
	return out, nil
}
