package main

import (
	"database/sql"
	"fmt"
	"io"
	"net/http"
	"os"
	"sync"
	"testing"
)

// Тесты с базой идут, когда задан TEST_DATABASE_URL, например
// postgresql://postgres@localhost:5432/paas_test. База очищается.
func testDB(t *testing.T) *sql.DB {
	t.Helper()
	url := os.Getenv("TEST_DATABASE_URL")
	if url == "" {
		t.Skip("TEST_DATABASE_URL не задан")
	}
	db, err := openDB(url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = db.Close() })
	if _, err := db.ExecContext(t.Context(), "DROP TABLE IF EXISTS page_visits, goose_db_version"); err != nil {
		t.Fatal(err)
	}
	return db
}

func TestConcurrentMigrationsApplyOnce(t *testing.T) {
	db := testDB(t)
	errs := make([]error, 5)
	var wg sync.WaitGroup
	for i := range errs {
		wg.Go(func() {
			// У каждого экземпляра своё подключение, как у отдельных процессов.
			instance, err := openDB(os.Getenv("TEST_DATABASE_URL"))
			if err == nil {
				err = migrate(t.Context(), instance, io.Discard)
				_ = instance.Close()
			}
			errs[i] = err
		})
	}
	wg.Wait()
	for _, err := range errs {
		if err != nil {
			t.Fatal(err)
		}
	}
	var applied int
	err := db.QueryRowContext(t.Context(),
		"SELECT count(*) FROM goose_db_version WHERE version_id = 1 AND is_applied").Scan(&applied)
	if err != nil {
		t.Fatal(err)
	}
	if applied != 1 {
		t.Fatalf("applied = %d, want 1", applied)
	}
}

func TestHomeCountsVisits(t *testing.T) {
	db := testDB(t)
	if err := migrate(t.Context(), db, io.Discard); err != nil {
		t.Fatal(err)
	}
	app, _, _ := setupWithCounter(t, newVisitCounter(db))
	// HEAD присылают проверки платформы, посещением он не считается.
	request(t, app, http.MethodHead, "/")
	for visit := 1; visit <= 2; visit++ {
		expect(t, get(t, app, "/"), http.StatusOK, "text/html; charset=utf-8",
			fmt.Sprintf("<h1>Привет от Хекслета!</h1><p>Первая &lt;публикация&gt;</p><p>Посещений: %d</p>\n", visit))
	}
}
