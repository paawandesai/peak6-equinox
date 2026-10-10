package web

import (
	"net/http"
)

// handleVersion returns the deployed git commit and build time (issue #80).
// It exposes only the commit and build time, never secrets (AB-06).
func (s *Server) handleVersion(w http.ResponseWriter, _ *http.Request) {
	resp := map[string]string{
		"commit": s.deps.AppVersion,
	}
	if s.deps.AppBuildTime != "" {
		resp["build_time"] = s.deps.AppBuildTime
	}
	s.writeJSON(w, http.StatusOK, resp)
}
