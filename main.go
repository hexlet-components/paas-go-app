// Учебное приложение курса «Деплой на PaaS».
package main

import (
	"fmt"
	"html"
	"io"
	"net/http"
	"os"
	"time"

	"github.com/getsentry/sentry-go"
)

const (
	heading      = "Привет от Хекслета!"
	errorMessage = "Oops! Something went wrong!"
)

// requestError даёт ошибке текст ответа дословно: errors.New со знаком «!» в
// конце не проходит проверку ST1005.
type requestError string

func (e requestError) Error() string { return string(e) }

func env(name, fallback string) string {
	if value := os.Getenv(name); value != "" {
		return value
	}
	return fallback
}

// newHandler собирает обработчик контракта. Журнал запросов пишется в out,
// ошибки уходят в Sentry через hub.
func newHandler(hub *sentry.Hub, out io.Writer) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		status := http.StatusOK
		contentType := "text/plain; charset=utf-8"
		var body string
		switch r.URL.Path {
		case "/":
			contentType = "text/html; charset=utf-8"
			message := html.EscapeString(env("SERVER_MESSAGE", "Hello, PaaS!"))
			body = "<h1>" + heading + "</h1><p>" + message + "</p>"
		case "/error":
			status = http.StatusInternalServerError
			body = errorMessage
			hub.CaptureException(requestError(errorMessage))
		default:
			status = http.StatusNotFound
			body = "not found"
		}
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = fmt.Fprintln(w, body)
		_, _ = fmt.Fprintf(out, "%s %s %d\n", r.Method, r.URL.Path, status)
	})
}

func newServer(port string, handler http.Handler) *http.Server {
	return &http.Server{
		Addr:              "0.0.0.0:" + port,
		Handler:           handler,
		ReadHeaderTimeout: 10 * time.Second,
	}
}

func main() {
	// Без SENTRY_DSN SDK ничего не отправляет.
	if err := sentry.Init(sentry.ClientOptions{Dsn: os.Getenv("SENTRY_DSN")}); err != nil {
		fmt.Fprintln(os.Stderr, "sentry:", err)
		os.Exit(1)
	}
	server := newServer(env("PORT", "3000"), newHandler(sentry.CurrentHub(), os.Stdout))
	fmt.Printf("Listening on http://%s\n", server.Addr)
	if err := server.ListenAndServe(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
