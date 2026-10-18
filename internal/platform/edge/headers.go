package edge

import (
	"crypto/rand"
	"encoding/base64"
	"net/http"
)

// NonceHeader carries a page's script nonce from the edge to the SSR runtime,
// which puts it on every script it writes.
const NonceHeader = "X-Notid-Nonce"

// withSecurityHeaders sets the headers every public response carries.
// /media and /proxy replace the CSP with their own, stricter one.
func withSecurityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("X-Frame-Options", "DENY")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=(), usb=()")
		h.Set("Content-Security-Policy", "default-src 'none'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// withPageCSP gives each page a fresh script nonce and a CSP that allows only
// this origin and scripts that carry the nonce.
func withPageCSP(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b := make([]byte, 16)
		_, _ = rand.Read(b)
		nonce := base64.StdEncoding.EncodeToString(b)
		r.Header.Set(NonceHeader, nonce)
		w.Header().Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'nonce-"+nonce+"'; "+
			"style-src 'self' 'unsafe-inline'; img-src 'self' data: blob:; font-src 'self'; connect-src 'self'; "+
			"object-src 'none'; base-uri 'none'; form-action 'self'; frame-ancestors 'none'")
		next.ServeHTTP(w, r)
	})
}

// withHSTS tells browsers to use https only, once the app is served over TLS.
func withHSTS(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Strict-Transport-Security", "max-age=31536000")
		next.ServeHTTP(w, r)
	})
}
