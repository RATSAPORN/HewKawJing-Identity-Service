package configs

import (
	"context"
	"log"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"time"

	"github.com/jmoiron/sqlx"
	_ "github.com/lib/pq"
)

var DB *sqlx.DB

func SetupDatabase(appName *string) *sqlx.DB {

	appNameVal := "default"
	if appName != nil && *appName != "" {
		appNameVal = *appName
	}
	host := os.Getenv("DB_HOST")
	port := os.Getenv("DB_PORT")
	user := os.Getenv("DB_USER")
	password := os.Getenv("DB_PASSWORD")
	dbname := os.Getenv("DB_NAME")
	schema := os.Getenv("DB_SCHEMA")
	if host == "" {
		host = "localhost"
	}
	if port == "" {
		port = "5432"
	}
	if user == "" {
		user = "postgres"
	}
	if dbname == "" {
		dbname = "postgres"
	}
	if schema == "" {
		schema = "public"
	}
	if !regexp.MustCompile(`^[a-z_][a-z0-9_]*$`).MatchString(schema) {
		log.Print("DB_SCHEMA must be a lowercase PostgreSQL identifier")
		return nil
	}

	maxOpenConns := atoiDefault(os.Getenv("DB_MAX_OPEN_CONNS"), 20)
	maxIdleConns := atoiDefault(os.Getenv("DB_MAX_IDLE_CONNS"), 20)
	connMaxLifetimeMin := atoiDefault(os.Getenv("DB_CONN_MAX_LIFETIME_MIN"), 30)
	connMaxIdleTimeMin := atoiDefault(os.Getenv("DB_CONN_MAX_IDLE_TIME_MIN"), 10)

	sslMode := os.Getenv("DB_SSLMODE")
	if sslMode == "" {
		sslMode = "disable"
	}
	connectionURL := url.URL{Scheme: "postgres", Host: net.JoinHostPort(host, port),
		User: url.UserPassword(user, password), Path: "/" + dbname}
	query := url.Values{"sslmode": {sslMode}, "application_name": {appNameVal},
		"search_path": {schema}, "connect_timeout": {"5"}}
	connectionURL.RawQuery = query.Encode()
	dsn := connectionURL.String()

	db, err := sqlx.Open("postgres", dsn)
	if err != nil {
		log.Print("Invalid database configuration")
		return nil
	}

	sqlDB := db.DB
	sqlDB.SetMaxOpenConns(maxOpenConns)
	sqlDB.SetMaxIdleConns(maxIdleConns)
	// Set connection maxlifetime
	sqlDB.SetConnMaxIdleTime(time.Duration(connMaxIdleTimeMin) * time.Minute)
	sqlDB.SetConnMaxLifetime(time.Duration(connMaxLifetimeMin) * time.Minute)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		log.Printf("DB not reachable at startup: %v", err)
		_ = db.Close()
		return nil
	}

	log.Printf("Connected to DB (%s:%s) app=%s", host, port, appNameVal)
	DB = db
	return db
}

func atoiDefault(s string, def int) int {
	if s == "" {
		return def
	}
	n, err := strconv.Atoi(s)
	if err != nil {
		return def
	}
	return n
}
