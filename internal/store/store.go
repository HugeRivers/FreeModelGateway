package store

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	"github.com/free-model-gateway/fmg/internal/appdir"
	_ "modernc.org/sqlite"
)

type Store struct {
	db *sql.DB
}

func Open() (*Store, error) {
	dbPath := appdir.DBFile()
	db, err := sql.Open("sqlite", dbPath)
	if err != nil {
		return nil, fmt.Errorf("open sqlite %s: %w", dbPath, err)
	}
	db.SetMaxOpenConns(1)
	db.SetConnMaxLifetime(time.Hour)

	if err := migrate(db); err != nil {
		db.Close()
		return nil, fmt.Errorf("migrate: %w", err)
	}
	return &Store{db: db}, nil
}

func (s *Store) Close() error { return s.db.Close() }

func migrate(db *sql.DB) error {
	stmts := []string{
		`CREATE TABLE IF NOT EXISTS model_stats (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_id TEXT NOT NULL,
			model_id TEXT NOT NULL,
			timestamp INTEGER NOT NULL,
			success INTEGER NOT NULL DEFAULT 0,
			error INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			latency_ms INTEGER NOT NULL DEFAULT 0,
			last_error TEXT
		)`,
		`CREATE INDEX IF NOT EXISTS idx_stats_time ON model_stats(timestamp)`,
		`CREATE INDEX IF NOT EXISTS idx_stats_model ON model_stats(provider_id, model_id)`,
		`CREATE TABLE IF NOT EXISTS daily_summary (
			date TEXT PRIMARY KEY,
			total_requests INTEGER NOT NULL DEFAULT 0,
			success_count INTEGER NOT NULL DEFAULT 0,
			error_count INTEGER NOT NULL DEFAULT 0,
			input_tokens INTEGER NOT NULL DEFAULT 0,
			output_tokens INTEGER NOT NULL DEFAULT 0,
			avg_latency_ms INTEGER NOT NULL DEFAULT 0
		)`,
		`CREATE TABLE IF NOT EXISTS app_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			return err
		}
	}
	return nil
}

func (s *Store) RecordRequest(ctx context.Context, providerID, modelID string, success bool, inputTokens, outputTokens, latencyMs int64, lastError string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO model_stats (provider_id, model_id, timestamp, success, error, input_tokens, output_tokens, latency_ms, last_error)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		providerID, modelID, time.Now().Unix(), btoi(success), btoi(!success), inputTokens, outputTokens, latencyMs, lastError)
	return err
}

func (s *Store) GetStatsSince(ctx context.Context, since time.Time) ([]ModelStatRow, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT provider_id, model_id,
			SUM(success) as succ, SUM(error) as err,
			SUM(input_tokens) as in_tok, SUM(output_tokens) as out_tok,
			AVG(latency_ms) as avg_lat
		FROM model_stats
		WHERE timestamp >= ?
		GROUP BY provider_id, model_id`, since.Unix())
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []ModelStatRow
	for rows.Next() {
		var r ModelStatRow
		if err := rows.Scan(&r.ProviderID, &r.ModelID, &r.SuccessCount, &r.ErrorCount, &r.InputTokens, &r.OutputTokens, &r.AvgLatencyMs); err != nil {
			return nil, err
		}
		out = append(out, r)
	}
	return out, rows.Err()
}

func (s *Store) PruneBefore(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_stats WHERE timestamp < ?`, before.Unix())
	return err
}

func (s *Store) SetState(ctx context.Context, key, value string) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO app_state (key, value, updated_at) VALUES (?, ?, ?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`,
		key, value, time.Now().Unix())
	return err
}

func (s *Store) GetState(ctx context.Context, key string) (string, error) {
	var val string
	err := s.db.QueryRowContext(ctx, `SELECT value FROM app_state WHERE key = ?`, key).Scan(&val)
	if err == sql.ErrNoRows {
		return "", nil
	}
	return val, err
}

type ModelStatRow struct {
	ProviderID   string
	ModelID      string
	SuccessCount int64
	ErrorCount   int64
	InputTokens  int64
	OutputTokens int64
	AvgLatencyMs int64
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) DB() *sql.DB { return s.db }
