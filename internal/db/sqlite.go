package db

import (
	"database/sql"
	"embed"
	"fmt"
	"path/filepath"
	"sort"
	"strings"

	_ "github.com/mattn/go-sqlite3"
)

//go:embed migrations/*.sql
var migrationFiles embed.FS

type DB struct {
	conn *sql.DB
}

func NewDB(dbPath string) (*DB, error) {
	conn, err := sql.Open("sqlite3", dbPath+"?_foreign_keys=on")
	if err != nil {
		return nil, fmt.Errorf("failed to open database: %w", err)
	}

	db := &DB{conn: conn}
	
	if err := db.runMigrations(); err != nil {
		conn.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return db, nil
}

func (db *DB) Close() error {
	return db.conn.Close()
}

func (db *DB) runMigrations() error {
	// Create migrations table if it doesn't exist
	if err := db.createMigrationsTable(); err != nil {
		return err
	}

	// Get migration files
	files, err := migrationFiles.ReadDir("migrations")
	if err != nil {
		return fmt.Errorf("failed to read migration files: %w", err)
	}

	// Sort migration files by name
	sort.Slice(files, func(i, j int) bool {
		return files[i].Name() < files[j].Name()
	})

	// Run each migration
	for _, file := range files {
		if !strings.HasSuffix(file.Name(), ".sql") {
			continue
		}

		migrationName := strings.TrimSuffix(file.Name(), ".sql")
		
		// Check if migration has already been applied
		applied, err := db.isMigrationApplied(migrationName)
		if err != nil {
			return fmt.Errorf("failed to check migration status: %w", err)
		}
		
		if applied {
			continue
		}

		// Read and execute migration
		content, err := migrationFiles.ReadFile(filepath.Join("migrations", file.Name()))
		if err != nil {
			return fmt.Errorf("failed to read migration %s: %w", file.Name(), err)
		}

		if _, err := db.conn.Exec(string(content)); err != nil {
			return fmt.Errorf("failed to execute migration %s: %w", file.Name(), err)
		}

		// Mark migration as applied
		if err := db.markMigrationApplied(migrationName); err != nil {
			return fmt.Errorf("failed to mark migration as applied: %w", err)
		}
	}

	return nil
}

func (db *DB) createMigrationsTable() error {
	query := `
		CREATE TABLE IF NOT EXISTS schema_migrations (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			migration_name TEXT UNIQUE NOT NULL,
			applied_at TIMESTAMP DEFAULT CURRENT_TIMESTAMP
		);
	`
	_, err := db.conn.Exec(query)
	return err
}

func (db *DB) isMigrationApplied(migrationName string) (bool, error) {
	query := "SELECT COUNT(*) FROM schema_migrations WHERE migration_name = ?"
	var count int
	err := db.conn.QueryRow(query, migrationName).Scan(&count)
	if err != nil {
		return false, err
	}
	return count > 0, nil
}

func (db *DB) markMigrationApplied(migrationName string) error {
	query := "INSERT INTO schema_migrations (migration_name) VALUES (?)"
	_, err := db.conn.Exec(query, migrationName)
	return err
}

// Health check method
func (db *DB) Ping() error {
	return db.conn.Ping()
}