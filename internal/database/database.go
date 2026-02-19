package database

import (
	"context"
	"database/sql"
	"fmt"
	"log"
	"strings"

	"github.com/bananalabs-oss/bunch/internal/models"
	"github.com/uptrace/bun"
	"github.com/uptrace/bun/dialect/sqlitedialect"
	_ "modernc.org/sqlite"
)

func Connect(databaseURL string) (*bun.DB, error) {
	path := strings.TrimPrefix(databaseURL, "sqlite://")

	sqldb, err := sql.Open("sqlite", path)
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite: %w", err)
	}

	if _, err := sqldb.Exec("PRAGMA journal_mode=WAL"); err != nil {
		return nil, fmt.Errorf("failed to set WAL mode: %w", err)
	}

	if _, err := sqldb.Exec("PRAGMA foreign_keys=ON"); err != nil {
		return nil, fmt.Errorf("failed to enable foreign keys: %w", err)
	}

	db := bun.NewDB(sqldb, sqlitedialect.New())

	if err := db.Ping(); err != nil {
		return nil, fmt.Errorf("failed to ping database: %w", err)
	}

	log.Printf("Connected to SQLite: %s", path)
	return db, nil
}

func Migrate(ctx context.Context, db *bun.DB) error {
	log.Printf("Running database migrations...")

	tables := []interface{}{
		(*models.Friendship)(nil),
		(*models.Block)(nil),
	}

	for _, model := range tables {
		_, err := db.NewCreateTable().
			Model(model).
			IfNotExists().
			Exec(ctx)
		if err != nil {
			return fmt.Errorf("failed to create table for %T: %w", model, err)
		}
	}

	indexes := []struct {
		name  string
		query string
	}{
		{
			"idx_friendships_requester",
			"CREATE INDEX IF NOT EXISTS idx_friendships_requester ON friendships (requester_id)",
		},
		{
			"idx_friendships_addressee",
			"CREATE INDEX IF NOT EXISTS idx_friendships_addressee ON friendships (addressee_id)",
		},
		{
			"idx_friendships_status",
			"CREATE INDEX IF NOT EXISTS idx_friendships_status ON friendships (status)",
		},
		{
			"idx_friendships_pair",
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_friendships_pair ON friendships (requester_id, addressee_id)",
		},
		{
			"idx_blocks_blocker",
			"CREATE INDEX IF NOT EXISTS idx_blocks_blocker ON blocks (blocker_id)",
		},
		{
			"idx_blocks_pair",
			"CREATE UNIQUE INDEX IF NOT EXISTS idx_blocks_pair ON blocks (blocker_id, blocked_id)",
		},
	}

	for _, idx := range indexes {
		if _, err := db.ExecContext(ctx, idx.query); err != nil {
			return fmt.Errorf("failed to create index %s: %w", idx.name, err)
		}
	}

	log.Printf("Migrations complete")
	return nil
}
