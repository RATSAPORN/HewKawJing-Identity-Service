package configs

import (
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/jmoiron/sqlx"
)

func RunMigrations(db *sqlx.DB, mode string, step int) {

	if mode == "down" {
		runDownMigrations(db, step)
		return
	}
	// table log
	_, err := db.Exec(`
		CREATE TABLE IF NOT EXISTS migration_logs (
			id SERIAL PRIMARY KEY,
			filename TEXT UNIQUE NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatalf("Failed to create migration_logs table: %v", err)
	}

	// Ensure migrations folder exists
	if _, err := os.Stat("migrations"); os.IsNotExist(err) {
		err := os.MkdirAll("migrations", 0755)
		if err != nil {
			log.Fatalf("Failed to create migrations folder: %v", err)
		}
		log.Println("Created migrations folder.")
	}

	// Read all migration files
	files, err := os.ReadDir("migrations")
	if err != nil {
		log.Fatalf("Failed to read migrations folder: %v", err)
	}

	// คัดเฉพาะ .up.sql แล้วเรียงลำดับ
	var fileNames []string
	for _, file := range files {
		name := file.Name()
		if strings.HasSuffix(name, ".up.sql") {
			fileNames = append(fileNames, name)
		}
	}
	sort.Strings(fileNames)

	var dbFilenames []string
	err = db.Select(&dbFilenames, `SELECT filename FROM migration_logs`)
	if err != nil {
		log.Fatalf("Failed to query migration_logs: %v", err)
	}

	// เช็คว่าไฟล์ไหน migrate ไปแล้ว
	for _, fname := range fileNames {
		exists := contains(dbFilenames, fname)

		// ถ้ามีอยู่แล้ว ไม่ต้อง migrate
		if exists {
			continue
		}

		// อ่านไฟล์ SQL แล้วรัน
		path := filepath.Join("migrations", fname)
		content, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("Failed to read file %s: %v", fname, err)
		}

		tx, err := db.Beginx()
		if err != nil {
			log.Fatalf("Failed to start migration %s: %v", fname, err)
		}
		_, err = tx.Exec(string(content))
		if err != nil {
			_ = tx.Rollback()
			log.Fatalf("Migration failed for %s: %v", fname, err)
		}

		// Insert log
		_, err = tx.Exec(`INSERT INTO migration_logs (filename, applied_at) VALUES ($1, current_timestamp)`, fname)
		if err != nil {
			_ = tx.Rollback()
			log.Fatalf("Failed to log migration %s: %v", fname, err)
		}
		if err := tx.Commit(); err != nil {
			log.Fatalf("Failed to commit migration %s: %v", fname, err)
		}

		log.Printf("Applied: %s", fname)
	}
	log.Println("All pending migrations applied successfully.")
}

func runDownMigrations(db *sqlx.DB, steps int) {
	// ดึง migration logs ล่าสุด
	var applied []string
	err := db.Select(&applied, `
		SELECT filename FROM migration_logs
		ORDER BY applied_at DESC
		LIMIT $1
	`, steps)
	if err != nil {
		log.Fatalf("Failed to fetch migration logs: %v", err)
	}

	for _, upFile := range applied {
		downFile := strings.Replace(upFile, ".up.sql", ".down.sql", 1)
		path := filepath.Join("migrations", downFile)

		content, err := os.ReadFile(path)
		if err != nil {
			log.Fatalf("Failed to read down file %s: %v", downFile, err)
		}

		_, err = db.Exec(string(content))
		if err != nil {
			log.Fatalf("Rollback failed for %s: %v", downFile, err)
		}

		// Remove log
		_, err = db.Exec(`DELETE FROM migration_logs WHERE filename = $1`, upFile)
		if err != nil {
			log.Fatalf("Failed to delete migration log %s: %v", upFile, err)
		}

		log.Printf("Rolled back: %s", upFile)
	}

	log.Printf("Rolled back %d migrations.", len(applied))
}

func contains(slice []string, item string) bool {
	for _, s := range slice {
		if s == item {
			return true
		}
	}
	return false
}
