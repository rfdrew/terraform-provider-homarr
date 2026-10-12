package client

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

// fakeHomarr answers requests in process by "METHOD /path", through
// Options.Transport, so tests need no listening socket; unrouted requests get a
// 404. It records every call in order, and the last body sent to each route.
type fakeHomarr struct {
	mu       sync.Mutex
	handlers map[string]http.HandlerFunc
	calls    []string
	bodies   map[string][]byte
}

func newFakeHomarr() *fakeHomarr {
	return &fakeHomarr{handlers: map[string]http.HandlerFunc{}, bodies: map[string][]byte{}}
}

func (f *fakeHomarr) handle(route string, h http.HandlerFunc) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.handlers[route] = h
}

// reply answers route with a fixed status and body.
func (f *fakeHomarr) reply(route string, status int, body string) {
	f.replies(route, status, body)
}

// replies answers successive requests to route with bodies in turn, repeating
// the last one once they run out. It lets a list change between two reads, as
// GET /api/users does around a create.
func (f *fakeHomarr) replies(route string, status int, bodies ...string) {
	var mu sync.Mutex
	next := 0
	f.handle(route, func(w http.ResponseWriter, _ *http.Request) {
		mu.Lock()
		body := bodies[next]
		if next < len(bodies)-1 {
			next++
		}
		mu.Unlock()
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	})
}

func (f *fakeHomarr) RoundTrip(req *http.Request) (*http.Response, error) {
	route := req.Method + " " + req.URL.Path

	f.mu.Lock()
	f.calls = append(f.calls, route)
	if req.Body != nil {
		raw, _ := io.ReadAll(req.Body)
		f.bodies[route] = raw
		req.Body = io.NopCloser(strings.NewReader(string(raw)))
	}
	h := f.handlers[route]
	f.mu.Unlock()

	rec := httptest.NewRecorder()
	if h == nil {
		rec.WriteHeader(http.StatusNotFound)
		_, _ = io.WriteString(rec, `{"message":"Not found","code":"NOT_FOUND"}`)
	} else {
		h(rec, req)
	}
	return rec.Result(), nil
}

// routes returns the calls made so far, in order.
func (f *fakeHomarr) routes() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.calls...)
}

func (f *fakeHomarr) called(route string) bool {
	for _, c := range f.routes() {
		if c == route {
			return true
		}
	}
	return false
}

// body decodes the last JSON object sent to route.
func (f *fakeHomarr) body(t *testing.T, route string) map[string]any {
	t.Helper()
	f.mu.Lock()
	raw, ok := f.bodies[route]
	f.mu.Unlock()
	if !ok {
		t.Fatalf("no request body recorded for %s; calls: %v", route, f.routes())
	}
	var decoded map[string]any
	if err := json.Unmarshal(raw, &decoded); err != nil {
		t.Fatalf("request body for %s is not a JSON object: %v (body: %s)", route, err, raw)
	}
	return decoded
}

func (f *fakeHomarr) client(t *testing.T) *Client {
	t.Helper()
	c, err := New(Options{BaseURL: "http://homarr.test", APIKey: "id.token", Transport: f})
	if err != nil {
		t.Fatal(err)
	}
	return c
}
