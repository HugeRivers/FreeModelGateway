package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/free-model-gateway/fmg/internal/appdir"
	"github.com/free-model-gateway/fmg/internal/crypto"
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
		`CREATE TABLE IF NOT EXISTS rate_limit_usage (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			platform TEXT NOT NULL,
			model_id TEXT NOT NULL,
			kind TEXT CHECK(kind IN ('request', 'tokens')) NOT NULL,
			tokens INTEGER NOT NULL DEFAULT 0,
			created_at_ms INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limit_time ON rate_limit_usage(created_at_ms)`,
		`CREATE INDEX IF NOT EXISTS idx_rate_limit_model ON rate_limit_usage(platform, model_id)`,
		`CREATE TABLE IF NOT EXISTS rate_limit_cooldowns (
			platform TEXT NOT NULL,
			model_id TEXT NOT NULL,
			expires_at_ms INTEGER NOT NULL,
			PRIMARY KEY (platform, model_id)
		)`,
		`CREATE TABLE IF NOT EXISTS app_state (
			key TEXT PRIMARY KEY,
			value TEXT NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		// Provider and model configuration tables
		`CREATE TABLE IF NOT EXISTS provider_templates (
			id TEXT PRIMARY KEY,
			name TEXT NOT NULL,
			base_url TEXT NOT NULL,
			api_format TEXT NOT NULL DEFAULT 'openai-compatible',
			api_key_env TEXT,
			default_headers TEXT,
			is_builtin INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL
		)`,
		`ALTER TABLE provider_templates ADD COLUMN api_key_env TEXT`,
		`CREATE TABLE IF NOT EXISTS provider_instances (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			template_id TEXT NOT NULL,
			name TEXT NOT NULL,
			api_key TEXT,
			custom_headers TEXT,
			is_enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS models (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			provider_instance_id INTEGER NOT NULL,
			model_id TEXT NOT NULL,
			name TEXT NOT NULL,
			description TEXT,
			is_enabled INTEGER NOT NULL DEFAULT 1,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL,
			UNIQUE(provider_instance_id, model_id)
		)`,
		`CREATE INDEX IF NOT EXISTS idx_models_provider ON models(provider_instance_id)`,
		`CREATE INDEX IF NOT EXISTS idx_models_enabled ON models(is_enabled)`,
		`CREATE TABLE IF NOT EXISTS route_config (
			id INTEGER PRIMARY KEY CHECK (id = 1),
			mode TEXT NOT NULL DEFAULT 'auto',
			strategy TEXT NOT NULL DEFAULT 'balanced',
			forced_provider_id TEXT,
			forced_model_id TEXT,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE TABLE IF NOT EXISTS users (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			username TEXT NOT NULL UNIQUE,
			password_hash TEXT NOT NULL,
			nickname TEXT,
			avatar TEXT,
			role TEXT NOT NULL DEFAULT 'user' CHECK(role IN ('admin', 'user')),
			api_key TEXT NOT NULL UNIQUE,
			created_at INTEGER NOT NULL,
			updated_at INTEGER NOT NULL
		)`,
		`CREATE INDEX IF NOT EXISTS idx_users_api_key ON users(api_key)`,
	}
	for _, stmt := range stmts {
		if _, err := db.Exec(stmt); err != nil {
			// Ignore duplicate column errors for ALTER TABLE migrations
			if strings.Contains(err.Error(), "duplicate column name") {
				continue
			}
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
	AvgLatencyMs float64
}

func btoi(b bool) int {
	if b {
		return 1
	}
	return 0
}

func (s *Store) RecordRateLimitUsage(ctx context.Context, platform, modelID, kind string, tokens int64) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO rate_limit_usage (platform, model_id, kind, tokens, created_at_ms)
		VALUES (?, ?, ?, ?, ?)`,
		platform, modelID, kind, tokens, time.Now().UnixMilli())
	return err
}

func (s *Store) PruneRateLimitUsage(ctx context.Context, before time.Time) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM rate_limit_usage WHERE created_at_ms < ?`, before.UnixMilli())
	return err
}

// ==================== Provider Templates ====================

type ProviderTemplate struct {
	ID             string `json:"id"`
	Name           string `json:"name"`
	BaseURL        string `json:"base_url"`
	APIFormat      string `json:"api_format"`
	APIKeyEnv      string `json:"api_key_env,omitempty"`
	DefaultHeaders string `json:"default_headers,omitempty"`
	IsBuiltin      bool   `json:"is_builtin"`
}

func (s *Store) UpsertProviderTemplate(ctx context.Context, t *ProviderTemplate) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_templates (id, name, base_url, api_format, api_key_env, default_headers, is_builtin, created_at)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			name=excluded.name,
			base_url=excluded.base_url,
			api_format=excluded.api_format,
			api_key_env=excluded.api_key_env,
			default_headers=excluded.default_headers`,
		t.ID, t.Name, t.BaseURL, t.APIFormat, t.APIKeyEnv, t.DefaultHeaders, btoi(t.IsBuiltin), time.Now().Unix())
	return err
}

func (s *Store) GetProviderTemplates(ctx context.Context) ([]ProviderTemplate, error) {
	rows, err := s.db.QueryContext(ctx, `SELECT id, name, base_url, api_format, api_key_env, default_headers, is_builtin FROM provider_templates ORDER BY name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderTemplate
	for rows.Next() {
		var t ProviderTemplate
		var builtin int
		var apiKeyEnv sql.NullString
		if err := rows.Scan(&t.ID, &t.Name, &t.BaseURL, &t.APIFormat, &apiKeyEnv, &t.DefaultHeaders, &builtin); err != nil {
			return nil, err
		}
		t.APIKeyEnv = apiKeyEnv.String
		t.IsBuiltin = builtin == 1
		out = append(out, t)
	}
	return out, rows.Err()
}

// ==================== Provider Instances ====================

type ProviderInstance struct {
	ID            int64  `json:"id"`
	TemplateID    string `json:"template_id"`
	Name          string `json:"name"`
	APIKey        string `json:"api_key,omitempty"`
	CustomHeaders string `json:"custom_headers,omitempty"`
	IsEnabled     bool   `json:"is_enabled"`
}

func (s *Store) CreateProviderInstance(ctx context.Context, p *ProviderInstance) (int64, error) {
	encKey, err := crypto.Encrypt(p.APIKey)
	if err != nil {
		return 0, fmt.Errorf("encrypt api key: %w", err)
	}
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO provider_instances (template_id, name, api_key, custom_headers, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		p.TemplateID, p.Name, encKey, p.CustomHeaders, btoi(p.IsEnabled), time.Now().Unix(), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateProviderInstance(ctx context.Context, p *ProviderInstance) error {
	encKey, err := crypto.Encrypt(p.APIKey)
	if err != nil {
		return fmt.Errorf("encrypt api key: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE provider_instances SET
			name=?, api_key=?, custom_headers=?, is_enabled=?, updated_at=?
		WHERE id=?`,
		p.Name, encKey, p.CustomHeaders, btoi(p.IsEnabled), time.Now().Unix(), p.ID)
	return err
}

func (s *Store) CountModelsByProvider(ctx context.Context, providerInstanceID int64) (int, error) {
	var count int
	err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM models WHERE provider_instance_id=?`, providerInstanceID).Scan(&count)
	return count, err
}

func (s *Store) DeleteProviderInstance(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM models WHERE provider_instance_id=?`, id)
	if err != nil {
		return fmt.Errorf("delete models: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `DELETE FROM provider_instances WHERE id=?`, id)
	return err
}

func (s *Store) GetProviderInstances(ctx context.Context) ([]ProviderInstance, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, template_id, name, api_key, custom_headers, is_enabled
		FROM provider_instances ORDER BY template_id, id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ProviderInstance
	for rows.Next() {
		var p ProviderInstance
		var enabled int
		var encKey string
		if err := rows.Scan(&p.ID, &p.TemplateID, &p.Name, &encKey, &p.CustomHeaders, &enabled); err != nil {
			return nil, err
		}
		p.IsEnabled = enabled == 1
		decKey, err := crypto.Decrypt(encKey)
		if err != nil {
			decKey = ""
		}
		p.APIKey = decKey
		out = append(out, p)
	}
	return out, rows.Err()
}

// ==================== Models ====================

type Model struct {
	ID                 int64  `json:"id"`
	ProviderInstanceID int64  `json:"provider_instance_id"`
	ModelID            string `json:"model_id"`
	Name               string `json:"name"`
	Description        string `json:"description,omitempty"`
	IsEnabled          bool   `json:"is_enabled"`
}

func (s *Store) CreateModel(ctx context.Context, m *Model) (int64, error) {
	res, err := s.db.ExecContext(ctx, `
		INSERT INTO models (provider_instance_id, model_id, name, description, is_enabled, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		m.ProviderInstanceID, m.ModelID, m.Name, m.Description, btoi(m.IsEnabled), time.Now().Unix(), time.Now().Unix())
	if err != nil {
		return 0, err
	}
	return res.LastInsertId()
}

func (s *Store) UpdateModel(ctx context.Context, m *Model) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE models SET
			model_id=?, name=?, description=?, is_enabled=?, updated_at=?
		WHERE id=?`,
		m.ModelID, m.Name, m.Description, btoi(m.IsEnabled), time.Now().Unix(), m.ID)
	return err
}

func (s *Store) DeleteModel(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM models WHERE id=?`, id)
	return err
}

func (s *Store) GetModelsByProvider(ctx context.Context, providerInstanceID int64) ([]Model, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider_instance_id, model_id, name, description, is_enabled
		FROM models WHERE provider_instance_id=? ORDER BY name`, providerInstanceID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		var m Model
		var enabled int
		if err := rows.Scan(&m.ID, &m.ProviderInstanceID, &m.ModelID, &m.Name, &m.Description, &enabled); err != nil {
			return nil, err
		}
		m.IsEnabled = enabled == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

func (s *Store) GetModel(ctx context.Context, id int64) (*Model, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT id, provider_instance_id, model_id, name, description, is_enabled
		FROM models WHERE id=?`, id)
	var m Model
	var enabled int
	if err := row.Scan(&m.ID, &m.ProviderInstanceID, &m.ModelID, &m.Name, &m.Description, &enabled); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	m.IsEnabled = enabled == 1
	return &m, nil
}

func (s *Store) GetAllModels(ctx context.Context) ([]Model, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, provider_instance_id, model_id, name, description, is_enabled
		FROM models ORDER BY provider_instance_id, name`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Model
	for rows.Next() {
		var m Model
		var enabled int
		if err := rows.Scan(&m.ID, &m.ProviderInstanceID, &m.ModelID, &m.Name, &m.Description, &enabled); err != nil {
			return nil, err
		}
		m.IsEnabled = enabled == 1
		out = append(out, m)
	}
	return out, rows.Err()
}

type RouteConfig struct {
	Mode             string `json:"mode"`
	Strategy         string `json:"strategy"`
	ForcedProviderID string `json:"forced_provider_id,omitempty"`
	ForcedModelID    string `json:"forced_model_id,omitempty"`
	UpdatedAt        int64  `json:"updated_at"`
}

func (s *Store) GetRouteConfig(ctx context.Context) (*RouteConfig, error) {
	row := s.db.QueryRowContext(ctx, `
		SELECT mode, strategy, forced_provider_id, forced_model_id, updated_at
		FROM route_config WHERE id=1`)
	var cfg RouteConfig
	var forcedProv, forcedModel sql.NullString
	err := row.Scan(&cfg.Mode, &cfg.Strategy, &forcedProv, &forcedModel, &cfg.UpdatedAt)
	if err == sql.ErrNoRows {
		return &RouteConfig{Mode: "auto", Strategy: "balanced"}, nil
	}
	if err != nil {
		return nil, err
	}
	if forcedProv.Valid {
		cfg.ForcedProviderID = forcedProv.String
	}
	if forcedModel.Valid {
		cfg.ForcedModelID = forcedModel.String
	}
	return &cfg, nil
}

func (s *Store) SaveRouteConfig(ctx context.Context, cfg *RouteConfig) error {
	_, err := s.db.ExecContext(ctx, `
		INSERT INTO route_config (id, mode, strategy, forced_provider_id, forced_model_id, updated_at)
		VALUES (1, ?, ?, ?, ?, ?)
		ON CONFLICT(id) DO UPDATE SET
			mode=excluded.mode,
			strategy=excluded.strategy,
			forced_provider_id=excluded.forced_provider_id,
			forced_model_id=excluded.forced_model_id,
			updated_at=excluded.updated_at`,
		cfg.Mode, cfg.Strategy, cfg.ForcedProviderID, cfg.ForcedModelID, time.Now().Unix())
	return err
}

func (s *Store) DB() *sql.DB { return s.db }
