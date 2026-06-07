package store

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"time"

	"golang.org/x/crypto/bcrypt"
)

type User struct {
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	Nickname     string `json:"nickname"`
	Avatar       string `json:"avatar"`
	Role         string `json:"role"`
	APIKey       string `json:"api_key"`
	CreatedAt    int64  `json:"created_at"`
	UpdatedAt    int64  `json:"updated_at"`
}

func generateAPIKey() string {
	b := make([]byte, 32)
	rand.Read(b)
	return "fmg_" + hex.EncodeToString(b)
}

func hashPassword(password string) (string, error) {
	bytes, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	return string(bytes), err
}

func checkPassword(password, hash string) bool {
	err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(password))
	return err == nil
}

func (s *Store) CreateUser(ctx context.Context, username, password, nickname, role string) (*User, error) {
	hash, err := hashPassword(password)
	if err != nil {
		return nil, fmt.Errorf("hash password: %w", err)
	}

	now := time.Now().Unix()
	apiKey := generateAPIKey()

	res, err := s.db.ExecContext(ctx, `
		INSERT INTO users (username, password_hash, nickname, role, api_key, created_at, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, ?)`,
		username, hash, nickname, role, apiKey, now, now)
	if err != nil {
		return nil, err
	}

	id, _ := res.LastInsertId()
	return s.GetUserByID(ctx, id)
}

func scanUser(row *sql.Row) (*User, error) {
	var u User
	var nickname, avatar sql.NullString
	err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &nickname, &avatar, &u.Role, &u.APIKey, &u.CreatedAt, &u.UpdatedAt)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.Nickname = nickname.String
	u.Avatar = avatar.String
	return &u, nil
}

func scanUserRows(rows *sql.Rows) (*User, error) {
	var u User
	var nickname, avatar sql.NullString
	err := rows.Scan(&u.ID, &u.Username, &u.PasswordHash, &nickname, &avatar, &u.Role, &u.APIKey, &u.CreatedAt, &u.UpdatedAt)
	if err != nil {
		return nil, err
	}
	u.Nickname = nickname.String
	u.Avatar = avatar.String
	return &u, nil
}

func (s *Store) GetUserByID(ctx context.Context, id int64) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, nickname, avatar, role, api_key, created_at, updated_at
		FROM users WHERE id = ?`, id))
}

func (s *Store) GetUserByUsername(ctx context.Context, username string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, nickname, avatar, role, api_key, created_at, updated_at
		FROM users WHERE username = ?`, username))
}

func (s *Store) GetUserByAPIKey(ctx context.Context, apiKey string) (*User, error) {
	return scanUser(s.db.QueryRowContext(ctx, `
		SELECT id, username, password_hash, nickname, avatar, role, api_key, created_at, updated_at
		FROM users WHERE api_key = ?`, apiKey))
}

func (s *Store) ListUsers(ctx context.Context) ([]*User, error) {
	rows, err := s.db.QueryContext(ctx, `
		SELECT id, username, password_hash, nickname, avatar, role, api_key, created_at, updated_at
		FROM users ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var out []*User
	for rows.Next() {
		u, err := scanUserRows(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, u)
	}
	return out, rows.Err()
}

func (s *Store) UpdateUserProfile(ctx context.Context, id int64, nickname, avatar string) error {
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET nickname = ?, avatar = ?, updated_at = ?
		WHERE id = ?`,
		nickname, avatar, time.Now().Unix(), id)
	return err
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, newPassword string) error {
	hash, err := hashPassword(newPassword)
	if err != nil {
		return fmt.Errorf("hash password: %w", err)
	}
	_, err = s.db.ExecContext(ctx, `
		UPDATE users SET password_hash = ?, updated_at = ?
		WHERE id = ?`,
		hash, time.Now().Unix(), id)
	return err
}

func (s *Store) RegenerateUserAPIKey(ctx context.Context, id int64) (string, error) {
	newKey := generateAPIKey()
	_, err := s.db.ExecContext(ctx, `
		UPDATE users SET api_key = ?, updated_at = ?
		WHERE id = ?`,
		newKey, time.Now().Unix(), id)
	return newKey, err
}

func (s *Store) DeleteUser(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM users WHERE id = ?`, id)
	return err
}

func (s *Store) VerifyPassword(ctx context.Context, username, password string) (*User, error) {
	u, err := s.GetUserByUsername(ctx, username)
	if err != nil {
		return nil, err
	}
	if u == nil {
		return nil, nil
	}
	if !checkPassword(password, u.PasswordHash) {
		return nil, nil
	}
	return u, nil
}

func (s *Store) InitDefaultAdmin(ctx context.Context) error {
	u, err := s.GetUserByUsername(ctx, "admin")
	if err != nil {
		return err
	}
	if u != nil {
		return nil
	}

	_, err = s.CreateUser(ctx, "admin", "admin", "Administrator", "admin")
	return err
}
