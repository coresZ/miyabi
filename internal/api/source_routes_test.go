package api

import "testing"

// The web client reads and replaces the configured storage sources and drives
// the incremental library sync, so these four routes must stay registered even
// when the library wiring is merged.
func TestSettingsSourcesAndLibrarySyncRoutesStayRegistered(t *testing.T) {
	router := NewRouter(Dependencies{Access: NewAccessGateService("", "")})
	routes := make(map[string]bool)
	for _, route := range router.Routes() {
		routes[route.Method+" "+route.Path] = true
	}
	for _, route := range []string{
		"GET /api/settings/sources",
		"PUT /api/settings/sources",
		"POST /api/library/sync",
		"GET /api/library/pending",
	} {
		if !routes[route] {
			t.Errorf("route missing: %s", route)
		}
	}
}
