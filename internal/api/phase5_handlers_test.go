package api

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/poolforge/poolforge/internal/engine"
)

// mockEngine implements engine.EngineService for API tests
type mockEngine struct {
	pools          map[string]*engine.Pool
	startResult    *engine.StartPoolResult
	startErr       error
	stopErr        error
	autoStartErr   error
}

func (m *mockEngine) CreatePool(ctx context.Context, req engine.CreatePoolRequest) (*engine.Pool, error) { return nil, nil }
func (m *mockEngine) GetPool(ctx context.Context, poolID string) (*engine.Pool, error) { return nil, nil }
func (m *mockEngine) ListPools(ctx context.Context) ([]engine.PoolSummary, error) { return nil, nil }
func (m *mockEngine) GetPoolStatus(ctx context.Context, poolID string) (*engine.PoolStatus, error) { return nil, nil }
func (m *mockEngine) AddDisk(ctx context.Context, poolID string, disk string) error { return nil }
func (m *mockEngine) ReplaceDisk(ctx context.Context, poolID string, oldDisk string, newDisk string) error { return nil }
func (m *mockEngine) RemoveDisk(ctx context.Context, poolID string, disk string) error { return nil }
func (m *mockEngine) DeletePool(ctx context.Context, poolID string) error { return nil }
func (m *mockEngine) HandleDiskFailure(ctx context.Context, poolID string, disk string) error { return nil }
func (m *mockEngine) GetRebuildProgress(ctx context.Context, poolID string, arrayDevice string) (*engine.RebuildProgress, error) { return nil, nil }
func (m *mockEngine) ImportPool() (*engine.ImportResult, error) { return nil, nil }

func (m *mockEngine) CreateShare(ctx context.Context, poolID string, share engine.Share) error { return nil }
func (m *mockEngine) DeleteShare(ctx context.Context, poolID string, name string) error { return nil }
func (m *mockEngine) UpdateShare(ctx context.Context, poolID string, name string, share engine.Share) error { return nil }
func (m *mockEngine) CreateUser(ctx context.Context, poolID string, name, password string, globalAccess bool) (*engine.NASUser, error) { return nil, nil }
func (m *mockEngine) DeleteUser(ctx context.Context, poolID string, name string) error { return nil }
func (m *mockEngine) CreateSnapshot(ctx context.Context, poolID string, name string, expiresIn string) (*engine.Snapshot, error) { return nil, nil }
func (m *mockEngine) DeleteSnapshot(ctx context.Context, poolID string, name string) error { return nil }
func (m *mockEngine) ListSnapshots(ctx context.Context, poolID string) ([]engine.Snapshot, error) { return nil, nil }
func (m *mockEngine) SetSnapshotSchedule(ctx context.Context, poolID string, schedule engine.SnapshotSchedule) error { return nil }
func (m *mockEngine) MountSnapshot(ctx context.Context, poolID string, name string) (string, error) { return "", nil }
func (m *mockEngine) UnmountSnapshot(ctx context.Context, poolID string, name string) error { return nil }
func (m *mockEngine) RestoreSnapshot(ctx context.Context, poolID string, name string) error { return nil }
func (m *mockEngine) RenameSnapshot(ctx context.Context, poolID string, oldName, newName string) error { return nil }
func (m *mockEngine) SetDiskLabel(ctx context.Context, device string, label string) error { return nil }

func (m *mockEngine) StartPool(ctx context.Context, poolName string, force bool) (*engine.StartPoolResult, error) {
	if m.startErr != nil {
		return nil, m.startErr
	}
	return m.startResult, nil
}

func (m *mockEngine) StopPool(ctx context.Context, poolName string) error {
	return m.stopErr
}

func (m *mockEngine) SetAutoStart(ctx context.Context, poolName string, autoStart bool) error {
	return m.autoStartErr
}
func (m *mockEngine) AssembleArrays(ctx context.Context, poolName string) error { return nil }
func (m *mockEngine) ActivateLVM(ctx context.Context, poolName string) error { return nil }
func (m *mockEngine) MountPool(ctx context.Context, poolName string) error { return nil }

func newTestServer(eng *mockEngine) *Server {
	return NewWithAuth(eng, "admin", "secret")
}

func authReq(method, path string, body []byte) *http.Request {
	var req *http.Request
	if body != nil {
		req = httptest.NewRequest(method, path, bytes.NewReader(body))
	} else {
		req = httptest.NewRequest(method, path, nil)
	}
	req.SetBasicAuth("admin", "secret")
	return req
}

func noAuthReq(method, path string) *http.Request {
	return httptest.NewRequest(method, path, nil)
}

func TestStartPoolAPI_Success(t *testing.T) {
	eng := &mockEngine{
		startResult: &engine.StartPoolResult{
			PoolName: "mypool", MountPoint: "/mnt/test",
			ArrayResults: []engine.ArrayStartResult{{Device: "/dev/md0", TierIndex: 0, State: engine.ArrayHealthy}},
		},
	}
	srv := newTestServer(eng)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("POST", "/api/pools/mypool/start", nil))

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
	var resp map[string]interface{}
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "running" {
		t.Errorf("expected status=running, got %v", resp["status"])
	}
}

func TestStartPoolAPI_AlreadyRunning(t *testing.T) {
	eng := &mockEngine{startErr: fmt.Errorf("pool 'mypool' is already running")}
	srv := newTestServer(eng)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("POST", "/api/pools/mypool/start", nil))

	if w.Code != 409 {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestStartPoolAPI_NotFound(t *testing.T) {
	eng := &mockEngine{startErr: fmt.Errorf("pool 'x' not found")}
	srv := newTestServer(eng)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("POST", "/api/pools/x/start", nil))

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestStartPoolAPI_NoAuth(t *testing.T) {
	srv := newTestServer(&mockEngine{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, noAuthReq("POST", "/api/pools/mypool/start"))

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestStopPoolAPI_Success(t *testing.T) {
	srv := newTestServer(&mockEngine{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("POST", "/api/pools/mypool/stop", nil))

	if w.Code != 200 {
		t.Errorf("expected 200, got %d", w.Code)
	}
	var resp map[string]string
	json.Unmarshal(w.Body.Bytes(), &resp)
	if resp["status"] != "safe_to_power_down" {
		t.Errorf("expected safe_to_power_down, got %v", resp["status"])
	}
}

func TestStopPoolAPI_NotRunning(t *testing.T) {
	eng := &mockEngine{stopErr: fmt.Errorf("pool 'mypool' is not running")}
	srv := newTestServer(eng)
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("POST", "/api/pools/mypool/stop", nil))

	if w.Code != 409 {
		t.Errorf("expected 409, got %d", w.Code)
	}
}

func TestStopPoolAPI_NoAuth(t *testing.T) {
	srv := newTestServer(&mockEngine{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, noAuthReq("POST", "/api/pools/mypool/stop"))

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}

func TestAutoStartAPI_Success(t *testing.T) {
	srv := newTestServer(&mockEngine{})
	body, _ := json.Marshal(map[string]bool{"auto_start": false})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("PUT", "/api/pools/mypool/autostart", body))

	if w.Code != 200 {
		t.Errorf("expected 200, got %d: %s", w.Code, w.Body.String())
	}
}

func TestAutoStartAPI_InvalidBody(t *testing.T) {
	srv := newTestServer(&mockEngine{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("PUT", "/api/pools/mypool/autostart", []byte(`{}`)))

	if w.Code != 400 {
		t.Errorf("expected 400, got %d", w.Code)
	}
}

func TestAutoStartAPI_NotFound(t *testing.T) {
	eng := &mockEngine{autoStartErr: fmt.Errorf("pool 'x' not found")}
	srv := newTestServer(eng)
	body, _ := json.Marshal(map[string]bool{"auto_start": true})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, authReq("PUT", "/api/pools/x/autostart", body))

	if w.Code != 404 {
		t.Errorf("expected 404, got %d", w.Code)
	}
}

func TestAutoStartAPI_NoAuth(t *testing.T) {
	srv := newTestServer(&mockEngine{})
	w := httptest.NewRecorder()
	srv.ServeHTTP(w, noAuthReq("PUT", "/api/pools/mypool/autostart"))

	if w.Code != 401 {
		t.Errorf("expected 401, got %d", w.Code)
	}
}
