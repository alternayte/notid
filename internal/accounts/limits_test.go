package accounts_test

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/alternayte/notid/internal/accounts"
	"github.com/alternayte/notid/internal/actors"
	"github.com/alternayte/notid/internal/platform"
)

func TestAPersonsWritesAreRateLimitedAndReadsAreNot(t *testing.T) {
	h := accounts.LimitWrites(platform.NewBuckets(1, 2))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	me := &actors.Actor{ID: 7}
	send := func(method string, a *actors.Actor) int {
		r := httptest.NewRequest(method, "/api/votes", nil)
		if a != nil {
			r = r.WithContext(accounts.WithActor(r.Context(), a))
		}
		w := httptest.NewRecorder()
		h.ServeHTTP(w, r)
		return w.Code
	}
	got := []int{send(http.MethodPost, me), send(http.MethodPost, me), send(http.MethodPost, me), send(http.MethodGet, me), send(http.MethodPost, &actors.Actor{ID: 8})}
	want := []int{200, 200, 429, 200, 200}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("answers %v, want %v", got, want)
		}
	}
}
