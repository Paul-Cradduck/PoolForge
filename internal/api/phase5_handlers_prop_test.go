package api

import (
	"fmt"
	"net/http/httptest"
	"testing"

	"pgregory.net/rapid"
)

// Feature: poolforge-phase5-enclosure-support, Property 93: Authentication enforcement on Phase 5 endpoints
func TestPropertyP93_AuthEnforcement(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		endpoints := []struct{ method, path string }{
			{"POST", "/api/pools/test/start"},
			{"POST", "/api/pools/test/stop"},
			{"PUT", "/api/pools/test/autostart"},
		}
		idx := rapid.IntRange(0, len(endpoints)-1).Draw(t, "endpoint")
		ep := endpoints[idx]

		srv := newTestServer(&mockEngine{})
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, noAuthReq(ep.method, ep.path))

		if w.Code != 401 {
			t.Errorf("%s %s without auth: expected 401, got %d", ep.method, ep.path, w.Code)
		}
	})
}

// Feature: poolforge-phase5-enclosure-support, Property 94: 404 for non-existent pools
func TestPropertyP94_NotFoundForUnknownPools(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		poolName := rapid.StringMatching(`[a-z]{3,15}`).Draw(t, "poolName")

		endpoints := []struct{ method, path string }{
			{"POST", "/api/pools/" + poolName + "/start"},
			{"POST", "/api/pools/" + poolName + "/stop"},
		}
		idx := rapid.IntRange(0, len(endpoints)-1).Draw(t, "endpoint")
		ep := endpoints[idx]

		eng := &mockEngine{}
		if ep.method == "POST" && ep.path[len(ep.path)-5:] == "start" {
			eng.startErr = fmt.Errorf("pool '%s' not found", poolName)
		} else {
			eng.stopErr = fmt.Errorf("pool '%s' not found", poolName)
		}

		srv := newTestServer(eng)
		w := httptest.NewRecorder()
		srv.ServeHTTP(w, authReq(ep.method, ep.path, nil))

		if w.Code != 404 {
			t.Errorf("%s %s: expected 404, got %d", ep.method, ep.path, w.Code)
		}
	})
}
