package sendclipper

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

// call is one request the fake API received.
type call struct {
	Method string
	Path   string
	Query  map[string]string
	Header http.Header
	Body   []byte
}

func (c call) json(t *testing.T) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(c.Body, &m); err != nil {
		t.Fatalf("body is not a JSON object: %q", c.Body)
	}
	return m
}

// reply is one canned answer; a nil Reply means "close the connection".
type reply struct {
	Status  int
	Body    any // JSON-encoded unless it is a string or []byte
	Headers map[string]string
	Drop    bool // hang up without answering (a network error)
	Delay   time.Duration
}

type fakeAPI struct {
	t       *testing.T
	mu      sync.Mutex
	replies []reply
	calls   []call
	slept   []time.Duration
	srv     *httptest.Server
}

func newFake(t *testing.T, replies ...reply) (*Client, *fakeAPI) {
	t.Helper()
	f := &fakeAPI{t: t, replies: replies}
	f.srv = httptest.NewServer(http.HandlerFunc(f.serve))
	t.Cleanup(f.srv.Close)
	c := NewClient("sc_live_test", WithBaseURL(f.srv.URL))
	c.sleep = func(_ context.Context, d time.Duration) error {
		f.mu.Lock()
		f.slept = append(f.slept, d)
		f.mu.Unlock()
		return nil
	}
	return c, f
}

func (f *fakeAPI) serve(w http.ResponseWriter, r *http.Request) {
	body, _ := io.ReadAll(r.Body)
	q := map[string]string{}
	for k, v := range r.URL.Query() {
		q[k] = v[0]
	}
	f.mu.Lock()
	f.calls = append(f.calls, call{Method: r.Method, Path: r.URL.EscapedPath(), Query: q, Header: r.Header.Clone(), Body: body})
	if len(f.replies) == 0 {
		f.mu.Unlock()
		f.t.Errorf("unexpected request %s %s", r.Method, r.URL.Path)
		w.WriteHeader(599)
		return
	}
	rep := f.replies[0]
	f.replies = f.replies[1:]
	f.mu.Unlock()

	if rep.Delay > 0 {
		select {
		case <-time.After(rep.Delay):
		case <-r.Context().Done():
			return
		}
	}
	if rep.Drop {
		hj, ok := w.(http.Hijacker)
		if !ok {
			f.t.Fatal("cannot hijack")
		}
		conn, _, _ := hj.Hijack()
		_ = conn.Close()
		return
	}
	for k, v := range rep.Headers {
		w.Header().Set(k, v)
	}
	var out []byte
	switch b := rep.Body.(type) {
	case nil:
	case string:
		out = []byte(b)
	case []byte:
		out = b
	default:
		out, _ = json.Marshal(b)
		if w.Header().Get("Content-Type") == "" {
			w.Header().Set("Content-Type", "application/json")
		}
	}
	status := rep.Status
	if status == 0 {
		status = 200
	}
	w.WriteHeader(status)
	_, _ = w.Write(out)
}

// Calls is a copy of the calls so far, read under the lock (the server runs on its own goroutine).
func (f *fakeAPI) Calls() []call {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]call(nil), f.calls...)
}

// Slept is a copy of the backoff waits so far.
func (f *fakeAPI) Slept() []time.Duration {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]time.Duration(nil), f.slept...)
}

func ok(body any) reply { return reply{Status: 200, Body: body} }

func apiErr(status int, code, message string) reply {
	return reply{Status: status, Body: map[string]any{"error": map[string]any{"code": code, "message": message}}}
}

func asError(t *testing.T, err error) *Error {
	t.Helper()
	e, isErr := err.(*Error)
	if !isErr {
		t.Fatalf("want *Error, got %T: %v", err, err)
	}
	return e
}
