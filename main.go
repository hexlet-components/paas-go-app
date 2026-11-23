// Учебное приложение курса «Деплой на PaaS».
package main

import (
	"context"
	"fmt"
	"html"
	"io"
	"net"
	"os"

	"github.com/getsentry/sentry-go"
	"github.com/gofiber/fiber/v3"
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

// newApp собирает приложение Fiber с контрактом курса. Журнал запросов
// пишется в out, ошибки уходят в Sentry через hub. Без базы counter равен nil,
// и главная страница выходит без счётчика посещений.
func newApp(hub *sentry.Hub, out io.Writer, counter visitCounter) *fiber.App {
	// Строгие настройки маршрутизации: /Error и /error/ не считаются /error.
	app := fiber.New(fiber.Config{CaseSensitive: true, StrictRouting: true})

	// Журнал пишется после ответа, поэтому статус уже известен.
	app.Use(func(c fiber.Ctx) error {
		err := c.Next()
		_, _ = fmt.Fprintf(out, "%s %s %d\n", c.Method(), c.Path(), c.Response().StatusCode())
		return err
	})

	app.Get("/", func(c fiber.Ctx) error {
		message := html.EscapeString(env("SERVER_MESSAGE", "Hello, PaaS!"))
		visits := ""
		if counter != nil {
			total, err := counter(c.Context())
			if err != nil {
				// Сбой базы обрабатывается как ошибка /error: событие в Sentry и ответ 500.
				hub.CaptureException(err)
				return fail(c)
			}
			visits = fmt.Sprintf("<p>Посещений: %d</p>", total)
		}
		c.Set(fiber.HeaderContentType, fiber.MIMETextHTMLCharsetUTF8)
		return c.SendString("<h1>" + heading + "</h1><p>" + message + "</p>" + visits + "\n")
	})

	app.Get("/error", func(c fiber.Ctx) error {
		hub.CaptureException(requestError(errorMessage))
		return fail(c)
	})

	// Любой другой путь и метод.
	app.Use(func(c fiber.Ctx) error {
		c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
		return c.Status(fiber.StatusNotFound).SendString("not found\n")
	})

	return app
}

func fail(c fiber.Ctx) error {
	c.Set(fiber.HeaderContentType, fiber.MIMETextPlainCharsetUTF8)
	return c.Status(fiber.StatusInternalServerError).SendString(errorMessage + "\n")
}

func listenAddr(port string) string {
	return "0.0.0.0:" + port
}

func main() {
	// Без SENTRY_DSN SDK ничего не отправляет.
	if err := sentry.Init(sentry.ClientOptions{Dsn: os.Getenv("SENTRY_DSN")}); err != nil {
		fmt.Fprintln(os.Stderr, "sentry:", err)
		os.Exit(1)
	}
	// С DATABASE_URL приложение сначала применяет миграции и только потом
	// начинает принимать запросы. Без неё оно работает без базы.
	var counter visitCounter
	if url := os.Getenv("DATABASE_URL"); url != "" {
		db, err := openDB(url)
		if err == nil {
			err = migrate(context.Background(), db, os.Stdout)
		}
		if err != nil {
			fmt.Fprintln(os.Stderr, "database:", err)
			os.Exit(1)
		}
		counter = newVisitCounter(db)
	}
	addr := listenAddr(env("PORT", "3000"))
	app := newApp(sentry.CurrentHub(), os.Stdout, counter)
	// Стартовый баннер Fiber выключен, строку запуска печатает ListenerAddrFunc,
	// когда порт уже занят.
	listenConfig := fiber.ListenConfig{
		DisableStartupMessage: true,
		ListenerAddrFunc: func(net.Addr) {
			fmt.Printf("Listening on http://%s\n", addr)
		},
	}
	if err := app.Listen(addr, listenConfig); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
