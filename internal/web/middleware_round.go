package web

import (
	"context"
	"net/http"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// roundKey is the context key for the current round (pinned or latest).
type roundKey struct{}

// roundMiddleware loads the current round (pinned, else latest) once per request
// and attaches it to the context. NFR-04: when a round is pinned, all pages use
// that round's stored data and make zero outbound venue calls.
func (s *Server) roundMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if s.deps.Store == nil {
			next.ServeHTTP(w, r)
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), dbTimeout)
		defer cancel()

		round, err := s.deps.Store.CurrentRound(ctx)
		if err != nil && !isNotFound(err) {
			s.log.Error("loading current round for request", "error", err, "path", r.URL.Path)
			// Continue without round; pages handle missing round themselves
			next.ServeHTTP(w, r)
			return
		}
		// Attach round to context (even if not found - pages check for zero ID)
		ctx = context.WithValue(r.Context(), roundKey{}, round)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

// roundFromContext returns the current round from the request context, or a
// zero Round if none was loaded (not found, or store unavailable).
func roundFromContext(ctx context.Context) store.Round {
	r, _ := ctx.Value(roundKey{}).(store.Round)
	return r
}
