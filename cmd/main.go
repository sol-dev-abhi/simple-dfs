package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/lib/pq"
)

const (
	dbConnStr = "user=user password=password dbname=filedb sslmode=disable"
)

func main() {
	db, err := sql.Open("postgres", dbConnStr)
	if err != nil {
		log.Fatal(err)
	}
	defer db.Close()

	// Create migrations table if it doesn't exist
	_, err = db.Exec(`
		CREATE TABLE IF NOT EXISTS migrations (
			id SERIAL PRIMARY KEY,
			version INT NOT NULL UNIQUE,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		)
	`)
	if err != nil {
		log.Fatal(err)
	}

	// Get all migration files
	files, err := filepath.Glob("migrations/*.sql")
	if err != nil {
		log.Fatal(err)
	}

	// Sort migration files
	sort.Strings(files)

	// Apply migrations
	for _, file := range files {
		version := getVersionFromFilename(file)
		if !isMigrationApplied(db, version) {
			applyMigration(db, file, version)
		} else {
			fmt.Printf("Migration %d already applied, skipping\n", version)
		}
	}

	fmt.Println("All migrations applied successfully")
}

func getVersionFromFilename(filename string) int {
	base := filepath.Base(filename)
	version := strings.Split(base, "_")[0]
	var v int
	fmt.Sscanf(version, "%d", &v)
	return v
}

func isMigrationApplied(db *sql.DB, version int) bool {
	var count int
	err := db.QueryRow("SELECT COUNT(*) FROM migrations WHERE version = $1", version).Scan(&count)
	if err != nil {
		log.Fatal(err)
	}
	return count > 0
}

func applyMigration(db *sql.DB, file string, version int) {
	content, err := os.ReadFile(file)
	if err != nil {
		log.Fatal(err)
	}

	tx, err := db.Begin()
	if err != nil {
		log.Fatal(err)
	}

	_, err = tx.Exec(string(content))
	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	_, err = tx.Exec("INSERT INTO migrations (version) VALUES ($1)", version)
	if err != nil {
		tx.Rollback()
		log.Fatal(err)
	}

	err = tx.Commit()
	if err != nil {
		log.Fatal(err)
	}

	fmt.Printf("Applied migration %d\n", version)
}
