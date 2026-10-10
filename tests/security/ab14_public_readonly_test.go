package security

import (
	"net/http"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/web"
)

// publicPaths are the open routes the app serves or will serve (M1-09), plus
// a few probes. None of them may accept a mutating method; the only public
// POST is /route, which is pure computation (threat-model AB-14).
var publicPaths = []string{
	"/", "/healthz", "/health", "/pairs/1", "/pairs/kalshi:X", "/matches", "/static/app.css",
	"/rounds", "/rounds/1", "/instruments/kalshi:X", "/eval", "/eval/cell", "/history", "/index.html", "/robots.txt",
}

var mutatingMethods = []string{http.MethodPost, http.MethodPut, http.MethodPatch, http.MethodDelete}

// TestAB14_PublicRoutesRejectMutations: every public route answers a mutating
// method with 404 or 405, never 2xx or 3xx, with or without a body, and no
// collector round runs as a side effect.
func TestAB14_PublicRoutesRejectMutations(t *testing.T) {
	h, ing, _ := newAdminHandler(t, "ci")
	for _, p := range publicPaths {
		for _, m := range mutatingMethods {
			rec := send(h, m, p, "203.0.113.5", func(r *http.Request) {
				r.Header.Set("Content-Type", "application/x-www-form-urlencoded")
				r.Header.Set("Origin", "https://equinox.example")
			})
			if rec.Code != http.StatusNotFound && rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s: status %d, want 404 or 405", m, p, rec.Code)
			}
		}
	}
	if ing.calls.Load() != 0 {
		t.Error("a public request ran a collector round")
	}
}

// TestAB14_RouteIsTheOnlyPublicPost: POST /route is reserved for the routing
// simulation (M1-09). Until it exists it is a 404; once it exists it must not
// be 405. Either way it is the only non-admin path where a POST may succeed.
func TestAB14_RouteIsTheOnlyPublicPost(t *testing.T) {
	h, _, _ := newAdminHandler(t, "ci")
	rec := send(h, http.MethodPost, "/route", "203.0.113.5", nil)
	if rec.Code >= 300 && rec.Code != http.StatusNotFound && rec.Code != http.StatusBadRequest && rec.Code != http.StatusUnprocessableEntity {
		t.Errorf("POST /route: status %d; expected not-yet-implemented (404), a validation error, or success", rec.Code)
	}
}

// TestAB14_AdminIsTheOnlyMutatingSurface: the mutating admin route exists and
// demands credentials, so state changes are gated behind basic auth and
// nothing else is reachable anonymously.
func TestAB14_AdminIsTheOnlyMutatingSurface(t *testing.T) {
	h, ing, _ := newAdminHandler(t, "ci")
	if rec := send(h, http.MethodPost, "/admin/ingest", "203.0.113.5", nil); rec.Code != http.StatusUnauthorized {
		t.Errorf("anonymous POST /admin/ingest: status %d, want 401", rec.Code)
	}
	if ing.calls.Load() != 0 {
		t.Error("an anonymous request ran a round")
	}
	// A server without admin credentials configured cannot be mutated at all.
	bare := web.New(web.Config{}, web.Deps{Ingester: ing}).Handler()
	if rec := send(bare, http.MethodPost, "/admin/ingest", "203.0.113.5", basic("", "")); rec.Code != http.StatusUnauthorized {
		t.Errorf("unconfigured admin: status %d, want 401", rec.Code)
	}
}
