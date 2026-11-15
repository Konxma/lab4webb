package handler

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"device-registry/internal/domain"
	"device-registry/internal/repository"
	"github.com/google/uuid"
)

const sensor = `{"name":"Thermal Sensor","ip_address":"192.168.1.50","type":"sensor","status":"online"}`

func request(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	w := httptest.NewRecorder()
	r := httptest.NewRequest(method, path, strings.NewReader(body))
	r.Header.Set("Content-Type", "application/json")
	h.ServeHTTP(w, r)
	return w
}

func decode[T any](t *testing.T, w *httptest.ResponseRecorder, expected int) T {
	t.Helper()
	var value T
	if w.Code != expected || w.Header().Get("Content-Type") != "application/json" {
		t.Fatalf("Got %d %s %s, want %d application/json", w.Code, w.Header(), w.Body, expected)
	}
	if err := json.Unmarshal(w.Body.Bytes(), &value); err != nil {
		t.Fatal(err)
	}
	return value
}

func checkError(t *testing.T, w *httptest.ResponseRecorder, expected int) {
	t.Helper()
	body := decode[errorResponse](t, w, expected)
	if body.Status != expected || body.Error != http.StatusText(expected) || body.Message == "" || body.Timestamp.IsZero() {
		t.Fatalf("Unexpected error: %+v", body)
	}
}

func testRouter() http.Handler {
	return NewRouter(repository.NewMemoryDeviceRepository(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func TestDeviceLifecycle(t *testing.T) {
	h := testRouter()
	if w := request(h, "GET", devicesPath, ""); w.Code != 200 || strings.TrimSpace(w.Body.String()) != "[]" {
		t.Fatalf("Expected empty registry: %s", w.Body)
	}
	created := request(h, "POST", devicesPath, sensor)
	item := decode[domain.Device](t, created, 201)
	path := devicesPath + "/" + item.ID
	if id, err := uuid.Parse(item.ID); err != nil || id.Version() != 4 || item.UpdatedAt.IsZero() || created.Header().Get("Location") != path {
		t.Fatalf("Invalid creation response: %+v, %v", item, created.Header())
	}
	if read := decode[domain.Device](t, request(h, "GET", path, ""), 200); read != item {
		t.Fatalf("Read response differs: %+v", read)
	}
	put := `{"name":"Controller","ip_address":"2001:0db8::10","type":"CONTROLLER"}`
	updated := decode[domain.Device](t, request(h, "PUT", path, put), 200)
	if updated.ID != item.ID || updated.Status != "offline" || updated.Type != "controller" ||
		updated.IPAddress != "2001:db8::10" || updated.Name != "Controller" || updated.UpdatedAt.Before(item.UpdatedAt) {
		t.Fatalf("Invalid update response: %+v", updated)
	}
	if read := decode[domain.Device](t, request(h, "GET", path, ""), 200); read != updated {
		t.Fatalf("Updated device differs: %+v", read)
	}
	if w := request(h, "DELETE", path, ""); w.Code != 204 || w.Body.Len() != 0 {
		t.Fatalf("Expected empty 204 response: %d %s", w.Code, w.Body)
	}
	checkError(t, request(h, "GET", path, ""), 404)
	checkError(t, request(h, "PUT", path, put), 404)
	checkError(t, request(h, "DELETE", path, ""), 404)
}

func TestFilteringAndInput(t *testing.T) {
	h := testRouter()
	decode[domain.Device](t, request(h, "POST", devicesPath+"/", sensor), 201)
	decode[domain.Device](t, request(h, "POST", devicesPath, strings.Replace(sensor, "sensor", "gateway", 1)), 201)
	decode[domain.Device](t, request(h, "POST", devicesPath, strings.Replace(sensor, "online", "offline", 1)), 201)
	for query, count := range map[string]int{
		"": 3, "?status=online": 2, "?type=sensor": 2, "?type=SENSOR&status=online": 1, "?type=relay": 0,
	} {
		items := decode[[]domain.Device](t, request(h, "GET", devicesPath+query, ""), 200)
		if items == nil || len(items) != count {
			t.Fatalf("Filter %q: got %v, want %d items", query, items, count)
		}
	}
	checkError(t, request(h, "GET", devicesPath+"?status=asleep", ""), 400)
	for _, body := range []string{
		"", "null", "[]", "{", "{}", sensor + "{}", strings.Replace(sensor, "192.168.1.50", "invalid-ip", 1),
		strings.Replace(sensor, "online", "idle", 1), strings.Replace(sensor, "status", "unknown_field", 1),
	} {
		checkError(t, request(h, "POST", devicesPath, body), 400)
		checkError(t, request(h, "PUT", devicesPath+"/unknown", body), 400)
	}
	checkError(t, request(h, "POST", devicesPath, sensor+strings.Repeat(" ", 16*1024)), 413)
}

type panicOnceRepository struct {
	repository.DeviceRepository
	panicked bool
}

func (r *panicOnceRepository) GetAll() []domain.Device {
	if !r.panicked {
		r.panicked = true
		panic("test panic")
	}
	return r.DeviceRepository.GetAll()
}

func TestMiddlewareAndRoutingErrors(t *testing.T) {
	var logs bytes.Buffer
	repo := &panicOnceRepository{DeviceRepository: repository.NewMemoryDeviceRepository()}
	h := NewRouter(repo, slog.New(slog.NewTextHandler(&logs, nil)))
	preflight := httptest.NewRequest("OPTIONS", devicesPath+"/known", nil)
	preflight.Header.Set("Origin", "https://panel.example")
	preflight.Header.Set("Access-Control-Request-Method", "PUT")
	preflight.Header.Set("Access-Control-Request-Headers", "Content-Type, Authorization")
	w := httptest.NewRecorder()
	h.ServeHTTP(w, preflight)
	if w.Code != 204 || w.Header().Get("Access-Control-Allow-Origin") != "*" ||
		!strings.Contains(w.Header().Get("Access-Control-Allow-Methods"), "PUT") ||
		w.Header().Get("Access-Control-Allow-Headers") != "Content-Type, Authorization" {
		t.Fatalf("Unexpected preflight response: %d %v", w.Code, w.Header())
	}
	w = request(h, "PATCH", devicesPath+"/known", "")
	checkError(t, w, 405)
	if w.Header().Get("Allow") != "GET, PUT, DELETE, OPTIONS" {
		t.Fatalf("Unexpected Allow header: %v", w.Header())
	}
	checkError(t, request(h, "GET", "/unknown", ""), 404)
	w = request(h, "GET", devicesPath, "")
	checkError(t, w, 500)
	if w.Header().Get("Access-Control-Allow-Origin") != "*" || strings.Contains(w.Body.String(), "test panic") {
		t.Fatalf("Unsafe or inaccessible recovery response: %v %s", w.Header(), w.Body)
	}
	decode[[]domain.Device](t, request(h, "GET", devicesPath, ""), 200)
	for _, entry := range []string{"Handler panic", "stack=", "test panic", "method=GET", "path=" + devicesPath, "client=", "status=500", "duration="} {
		if !strings.Contains(logs.String(), entry) {
			t.Fatalf("Missing %q in logs: %s", entry, &logs)
		}
	}
}
