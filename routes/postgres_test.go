package routes_test

import (
	"fmt"
	"identityService/configs"
	"identityService/repositories"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/joho/godotenv"
	"github.com/lib/pq"
)

// Opt in to PostgreSQL tests. A temporary schema isolates all test data.
func TestPostgresAuthFlow(t *testing.T) {
	if os.Getenv("IDENTITY_POSTGRES_TEST") != "1" {
		t.Skip("set IDENTITY_POSTGRES_TEST=1 to test PostgreSQL using .env")
	}
	t.Chdir("..")
	if err := godotenv.Load(".env"); err != nil && !os.IsNotExist(err) {
		t.Fatal("Cannot load .env; check its KEY=value syntax")
	}
	db := configs.SetupDatabase(nil)
	if db == nil {
		t.Fatal("PostgreSQL is unavailable")
	}
	t.Cleanup(func() { _ = db.Close() })
	db.SetMaxOpenConns(1)
	db.SetMaxIdleConns(1)
	schema := fmt.Sprintf("identity_test_%d", time.Now().UnixNano())
	quoted := pq.QuoteIdentifier(schema)
	if _, err := db.Exec("CREATE SCHEMA " + quoted); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if _, err := db.Exec("DROP SCHEMA " + quoted + " CASCADE"); err != nil {
			t.Errorf("remove test schema: %v", err)
		}
	})
	if _, err := db.Exec("SET search_path TO " + quoted); err != nil {
		t.Fatal(err)
	}
	// Exercise the actual migration runner, including a second idempotent startup.
	configs.RunMigrations(db, "up", 1)
	configs.RunMigrations(db, "up", 1)
	repo := repositories.NewIdentityRepository(db)
	exerciseAuthFlow(t, repo)
	router := testRouter(repo)

	auth := authData(t, request(t, router, "POST", "/login", loginBody, "", 200))
	if _, err := db.Exec("UPDATE auth_sessions SET access_expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	request(t, router, "GET", "/me", "", auth.AccessToken, 401)
	auth = authData(t, request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 200))
	if _, err := db.Exec("UPDATE auth_sessions SET expires_at = now() - interval '1 second'"); err != nil {
		t.Fatal(err)
	}
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 401)

	auth = authData(t, request(t, router, "POST", "/login", loginBody, "", 200))
	if _, err := db.Exec("UPDATE users SET account_status = 'disabled'"); err != nil {
		t.Fatal(err)
	}
	request(t, router, "POST", "/login", loginBody, "", 401)
	request(t, router, "GET", "/me", "", auth.AccessToken, 401)
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 401)
	if _, err := db.Exec("UPDATE users SET account_status = 'active', deleted_at = now()"); err != nil {
		t.Fatal(err)
	}
	request(t, router, "POST", "/login", loginBody, "", 401)
	request(t, router, "GET", "/me", "", auth.AccessToken, 401)
	request(t, router, "POST", "/refresh-token", `{"refresh_token":"`+auth.RefreshToken+`"}`, "", 401)
	if _, err := db.Exec("UPDATE users SET deleted_at = NULL"); err != nil {
		t.Fatal(err)
	}

	// One winner for simultaneous refresh requests using the same token.
	auth = authData(t, request(t, router, "POST", "/login", loginBody, "", 200))
	// Requests share the schema-bound connection and race to consume the same token.
	statuses := make(chan int, 2)
	for range 2 {
		go func() {
			req := httptest.NewRequest("POST", "/api/v1/identity/refresh-token",
				strings.NewReader(`{"refresh_token":"`+auth.RefreshToken+`"}`))
			rec := httptest.NewRecorder()
			router.ServeHTTP(rec, req)
			statuses <- rec.Code
		}()
	}
	a, b := <-statuses, <-statuses
	if !((a == 200 && b == 401) || (a == 401 && b == 200)) {
		t.Fatalf("refresh replay accepted: %d, %d", a, b)
	}
}
