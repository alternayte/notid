package federation

import (
	"crypto"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/pem"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"
)

// HTTP Signatures as peers use them: draft-cavage with rsa-sha256 and a
// SHA-256 Digest of the body. Mastodon and PieFed label the algorithm hs2019;
// the key decides the algorithm, so both labels mean RSA with SHA-256 here.

var (
	errNoSignature  = errors.New("request has no Signature header")
	errBadSignature = errors.New("signature does not verify")
)

func digestHeader(body []byte) string {
	sum := sha256.Sum256(body)
	return "SHA-256=" + base64.StdEncoding.EncodeToString(sum[:])
}

// signRequest signs req as keyID. A request with a body also carries its digest.
func signRequest(req *http.Request, keyID string, key *rsa.PrivateKey, body []byte, now time.Time) error {
	req.Header.Set("Date", now.UTC().Format(http.TimeFormat))
	headers := []string{"(request-target)", "host", "date"}
	if body != nil {
		req.Header.Set("Digest", digestHeader(body))
		headers = append(headers, "digest", "content-type")
	}
	signed, err := signingString(req, headers)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(signed))
	sig, err := rsa.SignPKCS1v15(nil, key, crypto.SHA256, sum[:])
	if err != nil {
		return fmt.Errorf("sign request: %w", err)
	}
	req.Header.Set("Signature", fmt.Sprintf(`keyId="%s",algorithm="rsa-sha256",headers="%s",signature="%s"`,
		keyID, strings.Join(headers, " "), base64.StdEncoding.EncodeToString(sig)))
	return nil
}

type signatureParams struct {
	KeyID     string
	Headers   []string
	Signature []byte
}

func parseSignature(r *http.Request) (signatureParams, error) {
	raw := r.Header.Get("Signature")
	if raw == "" {
		if auth := r.Header.Get("Authorization"); strings.HasPrefix(auth, "Signature ") {
			raw = strings.TrimPrefix(auth, "Signature ")
		}
	}
	if raw == "" {
		return signatureParams{}, errNoSignature
	}
	var p signatureParams
	algorithm := ""
	for _, part := range splitParams(raw) {
		k, v, ok := strings.Cut(part, "=")
		if !ok {
			continue
		}
		v = strings.Trim(strings.TrimSpace(v), `"`)
		switch strings.TrimSpace(k) {
		case "keyId":
			p.KeyID = v
		case "algorithm":
			algorithm = v
		case "headers":
			p.Headers = strings.Fields(strings.ToLower(v))
		case "signature":
			sig, err := base64.StdEncoding.DecodeString(v)
			if err != nil {
				return p, fmt.Errorf("signature is not base64: %w", err)
			}
			p.Signature = sig
		}
	}
	if algorithm != "" && algorithm != "rsa-sha256" && algorithm != "hs2019" {
		return p, fmt.Errorf("unsupported signature algorithm %q", algorithm)
	}
	if p.KeyID == "" || len(p.Signature) == 0 {
		return p, errors.New("signature lacks keyId or signature")
	}
	if len(p.Headers) == 0 {
		p.Headers = []string{"date"}
	}
	return p, nil
}

// splitParams splits on commas outside quotes.
func splitParams(s string) []string {
	var out []string
	inQuote, start := false, 0
	for i, c := range s {
		switch c {
		case '"':
			inQuote = !inQuote
		case ',':
			if !inQuote {
				out = append(out, s[start:i])
				start = i + 1
			}
		}
	}
	return append(out, s[start:])
}

func signingString(r *http.Request, headers []string) (string, error) {
	lines := make([]string, 0, len(headers))
	for _, h := range headers {
		var v string
		switch h {
		case "(request-target)":
			v = strings.ToLower(r.Method) + " " + r.URL.RequestURI()
		case "host":
			v = r.Host
			if v == "" {
				v = r.URL.Host
			}
		default:
			vals := r.Header.Values(h)
			if len(vals) == 0 {
				return "", fmt.Errorf("signed header %q is missing", h)
			}
			v = strings.Join(vals, ", ")
		}
		lines = append(lines, h+": "+strings.TrimSpace(v))
	}
	return strings.Join(lines, "\n"), nil
}

// checkSignedRequest checks everything except the key: which headers are
// signed, the date window and the body digest.
func checkSignedRequest(r *http.Request, p signatureParams, body []byte, now time.Time, maxSkew time.Duration) error {
	required := []string{"(request-target)", "host", "date"}
	if r.Method == http.MethodPost {
		required = append(required, "digest")
	}
	for _, h := range required {
		if !contains(p.Headers, h) {
			return fmt.Errorf("signature does not cover %q", h)
		}
	}
	date, err := http.ParseTime(r.Header.Get("Date"))
	if err != nil {
		return fmt.Errorf("bad Date header: %w", err)
	}
	if d := now.Sub(date); d > maxSkew || d < -maxSkew {
		return fmt.Errorf("date %s is outside the accepted window", date.Format(time.RFC3339))
	}
	if r.Method == http.MethodPost && !digestMatches(r.Header.Get("Digest"), body) {
		return errors.New("digest does not match the body")
	}
	return nil
}

func digestMatches(header string, body []byte) bool {
	want := strings.TrimPrefix(digestHeader(body), "SHA-256=")
	for _, part := range strings.Split(header, ",") {
		alg, v, ok := strings.Cut(strings.TrimSpace(part), "=")
		if ok && strings.EqualFold(alg, "SHA-256") && v == want {
			return true
		}
	}
	return false
}

func verifyWithKey(r *http.Request, p signatureParams, pemKey string) error {
	key, err := parsePublicKey(pemKey)
	if err != nil {
		return err
	}
	signed, err := signingString(r, p.Headers)
	if err != nil {
		return err
	}
	sum := sha256.Sum256([]byte(signed))
	if err := rsa.VerifyPKCS1v15(key, crypto.SHA256, sum[:], p.Signature); err != nil {
		return errBadSignature
	}
	return nil
}

func parsePublicKey(pemKey string) (*rsa.PublicKey, error) {
	block, _ := pem.Decode([]byte(pemKey))
	if block == nil {
		return nil, errors.New("public key is not PEM")
	}
	if block.Type == "RSA PUBLIC KEY" {
		return x509.ParsePKCS1PublicKey(block.Bytes)
	}
	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse public key: %w", err)
	}
	rk, ok := key.(*rsa.PublicKey)
	if !ok {
		return nil, errors.New("public key is not RSA")
	}
	return rk, nil
}

func contains(list []string, s string) bool {
	for _, v := range list {
		if v == s {
			return true
		}
	}
	return false
}

// keyOwner is the actor AP ID of a key ID: the key ID without its fragment.
func keyOwner(keyID string) string {
	if i := strings.IndexByte(keyID, '#'); i >= 0 {
		return keyID[:i]
	}
	return keyID
}
