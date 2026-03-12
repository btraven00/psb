package db

import (
	"database/sql"
	"fmt"

	"github.com/btraven00/psb/internal/models"
	_ "github.com/tursodatabase/libsql-client-go/libsql"
	"gorm.io/driver/postgres"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
)

// InitWith opens the database with explicit type and DSN parameters.
func InitWith(dbType, dsn string) (*gorm.DB, error) {
	var dialector gorm.Dialector
	switch dbType {
	case "sqlite":
		dialector = sqlite.Open(dsn + "?_journal_mode=WAL&_synchronous=NORMAL")
	case "turso":
		sqlDB, err := sql.Open("libsql", dsn)
		if err != nil {
			return nil, fmt.Errorf("failed to open turso connection: %w", err)
		}
		dialector = sqlite.New(sqlite.Config{Conn: sqlDB})
	case "postgres":
		dialector = postgres.Open(dsn)
	default:
		return nil, fmt.Errorf("unsupported DB_TYPE: %s", dbType)
	}

	db, err := gorm.Open(dialector, &gorm.Config{})
	if err != nil {
		return nil, fmt.Errorf("failed to connect to database: %w", err)
	}

	if err := db.AutoMigrate(&models.Session{}, &models.Environment{}, &models.ExecutionMetric{}); err != nil {
		return nil, fmt.Errorf("failed to migrate: %w", err)
	}

	return db, nil
}
