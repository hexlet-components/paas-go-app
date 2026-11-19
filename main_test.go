package main

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/getsentry/sentry-go"
)

// recorder подменяет транспорт Sentry и запоминает отправленные события.
type recorder struct {
	mu     sync.Mutex
	events []*sentry.Event
}

func (r *recorder) Configure(sentry.ClientOptions)        {}
func (r *recorder) Flush(time.Duration) bool              { return true }
func (r *recorder) FlushWithContext(context.Context) bool { return true }
func (r *recorder) Close()                                {}
func (r *recorder) SendEvent(event *sentry.Event) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, event)
}

func (r *recorder) count() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.events)
}

func setup(t *testing.T) (*httptest.Server, *recorder, *bytes.Buffer) {
	t.Helper()
	t.Setenv("SERVER_MESSAGE", "Первая <публикация>")
	transport := &recorder{}
	client, err := sentry.NewClient(sentry.ClientOptions{Transport: transport})
	if err != nil {
		t.Fatal(err)
	}
	hub := sentry.NewHub(client, sentry.NewScope())
	logs := &bytes.Buffer{}
	server := httptest.NewServer(newHandler(hub, logs))
	t.Cleanup(server.Close)
	return server, transport, logs
}

type result struct {
	status      int
	contentType string
	body        string
}

func get(t *testing.T, url string) result {
	t.Helper()
	request, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatal(err)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	body, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatal(err)
	}
	if err := response.Body.Close(); err != nil {
		t.Fatal(err)
	}
	return result{response.StatusCode, response.Header.Get("Content-Type"), string(body)}
}

func expect(t *testing.T, got result, status int, contentType, body string) {
	t.Helper()
	want := result{status, contentType, body}
	if got != want {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

func TestHomeShowsHeadingAndEscapedMessage(t *testing.T) {
	server, _, _ := setup(t)
	expect(t, get(t, server.URL+"/"), http.StatusOK, "text/html; charset=utf-8",
		"<h1>Привет от Хекслета!</h1><p>Первая &lt;публикация&gt;</p>\n")
}

func TestHomeDefaultMessage(t *testing.T) {
	server, _, _ := setup(t)
	t.Setenv("SERVER_MESSAGE", "")
	expect(t, get(t, server.URL+"/"), http.StatusOK, "text/html; charset=utf-8",
		"<h1>Привет от Хекслета!</h1><p>Hello, PaaS!</p>\n")
}

func TestErrorIsReportedAndServerKeepsWorking(t *testing.T) {
	server, transport, _ := setup(t)
	expect(t, get(t, server.URL+"/error"), http.StatusInternalServerError,
		"text/plain; charset=utf-8", "Oops! Something went wrong!\n")
	if transport.count() != 1 {
		t.Fatalf("events = %d, want 1", transport.count())
	}
	if got := transport.events[0].Exception[0].Value; got != "Oops! Something went wrong!" {
		t.Fatalf("event message = %q", got)
	}
	if got := get(t, server.URL+"/"); got.status != http.StatusOK {
		t.Fatalf("status after error = %d", got.status)
	}
}

func TestUnknownPath(t *testing.T) {
	server, _, _ := setup(t)
	expect(t, get(t, server.URL+"/missing"), http.StatusNotFound,
		"text/plain; charset=utf-8", "not found\n")
}

func TestRequestIsLogged(t *testing.T) {
	server, _, logs := setup(t)
	get(t, server.URL+"/missing")
	get(t, server.URL+"/")
	if got, want := logs.String(), "GET /missing 404\nGET / 200\n"; got != want {
		t.Fatalf("log = %q, want %q", got, want)
	}
}

func TestListensOnAllInterfaces(t *testing.T) {
	if got := newServer("3000", http.NotFoundHandler()).Addr; got != "0.0.0.0:3000" {
		t.Fatalf("addr = %q", got)
	}
}

func TestEnvFallback(t *testing.T) {
	t.Setenv("PORT", "")
	if got := env("PORT", "3000"); got != "3000" {
		t.Fatalf("PORT = %q", got)
	}
	t.Setenv("PORT", "8080")
	if got := env("PORT", "3000"); got != "8080" {
		t.Fatalf("PORT = %q", got)
	}
}
