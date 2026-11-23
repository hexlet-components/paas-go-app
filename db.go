package main

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	"io"
	"io/fs"

	// Драйвер pgx регистрируется в database/sql под именем pgx.
	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/pressly/goose/v3"
	"github.com/pressly/goose/v3/lock"
)

//go:embed migrations/*.sql
var embeddedMigrations embed.FS

// Один запрос и создаёт строку счётчика, и увеличивает её, поэтому
// одновременные запросы не теряют посещения.
const countVisitSQL = `INSERT INTO page_visits (path, total) VALUES ('/', 1)
ON CONFLICT (path) DO UPDATE SET total = page_visits.total + 1
RETURNING total`

// visitCounter увеличивает счётчик главной страницы и возвращает новое значение.
type visitCounter func(ctx context.Context) (int64, error)

func openDB(url string) (*sql.DB, error) {
	return sql.Open("pgx", url)
}

// migrate применяет новые миграции из каталога migrations. Сессионная
// advisory-блокировка goose не даёт двум экземплярам приложения применять
// миграции одновременно: второй ждёт, пока первый закончит.
func migrate(ctx context.Context, db *sql.DB, out io.Writer) error {
	locker, err := lock.NewPostgresSessionLocker()
	if err != nil {
		return err
	}
	migrations, err := fs.Sub(embeddedMigrations, "migrations")
	if err != nil {
		return err
	}
	provider, err := goose.NewProvider(goose.DialectPostgres, db, migrations, goose.WithSessionLocker(locker))
	if err != nil {
		return err
	}
	results, err := provider.Up(ctx)
	if err != nil {
		return err
	}
	for _, result := range results {
		_, _ = fmt.Fprintf(out, "Applied migration %s\n", result.Source.Path)
	}
	_, _ = fmt.Fprintln(out, "Migrations are up to date")
	return nil
}

func newVisitCounter(db *sql.DB) visitCounter {
	return func(ctx context.Context) (int64, error) {
		var total int64
		err := db.QueryRowContext(ctx, countVisitSQL).Scan(&total)
		return total, err
	}
}
