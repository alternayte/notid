package federation

import (
	"net/http"
)

const nodeInfoSchema = "http://nodeinfo.diaspora.software/ns/schema/2.1"

// nodeInfoIndex points clients at the 2.1 document.
func (s *Server) nodeInfoIndex(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	writeJSONBody(w, map[string]any{
		"links": []any{
			map[string]string{"rel": nodeInfoSchema, "href": s.origin(r) + "/nodeinfo/2.1"},
		},
	})
}

// nodeInfo describes this installation.
func (s *Server) nodeInfo(w http.ResponseWriter, r *http.Request) {
	var users int64
	if err := s.Pool.QueryRow(r.Context(), `SELECT count(*) FROM actors WHERE local AND kind = 'person'`).Scan(&users); err != nil {
		s.Log.ErrorContext(r.Context(), "nodeinfo count", "err", err)
		http.Error(w, "count failed", http.StatusInternalServerError)
		return
	}
	body := map[string]any{
		"version": "2.1",
		"software": map[string]string{
			"name":    "notid",
			"version": s.Version,
		},
		"protocols":         []string{"activitypub"},
		"services":          map[string][]string{"inbound": {}, "outbound": {}},
		"openRegistrations": !s.SignupClosed,
		"usage": map[string]any{
			"users": map[string]int64{"total": users},
		},
		"metadata": map[string]any{},
	}
	w.Header().Set("Content-Type", "application/json")
	writeJSONBody(w, body)
}
