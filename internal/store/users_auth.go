package store

import (
	"context"
	"errors"
	"strings"

	"github.com/jackc/pgx/v5"
)

const (
	RoleAdmin  = "admin"
	RoleDeploy = "deploy"
	RoleRead   = "read"
)

type UserAccount struct {
	ID           string
	Login        string
	Role         string
	PasswordHash string
	TOTPSecret   string
}

func (s *Store) UserAccount(ctx context.Context, id string) (UserAccount, error) {
	var row UserAccount
	err := s.pool.QueryRow(ctx, `
		SELECT id, login, role, password_hash, totp_secret FROM users WHERE id = $1`, id).
		Scan(&row.ID, &row.Login, &row.Role, &row.PasswordHash, &row.TOTPSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	return row, err
}

func (s *Store) UserByLogin(ctx context.Context, login string) (UserAccount, error) {
	login = strings.TrimSpace(strings.ToLower(login))
	var row UserAccount
	err := s.pool.QueryRow(ctx, `
		SELECT id, login, role, password_hash, totp_secret FROM users WHERE lower(login) = $1`, login).
		Scan(&row.ID, &row.Login, &row.Role, &row.PasswordHash, &row.TOTPSecret)
	if errors.Is(err, pgx.ErrNoRows) {
		return row, ErrNotFound
	}
	return row, err
}

func (s *Store) ListUserAccounts(ctx context.Context) ([]UserAccount, error) {
	rows, err := s.pool.Query(ctx, `SELECT id, login, role, password_hash, totp_secret FROM users ORDER BY created_at`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []UserAccount
	for rows.Next() {
		var row UserAccount
		if err := rows.Scan(&row.ID, &row.Login, &row.Role, &row.PasswordHash, &row.TOTPSecret); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}

func (s *Store) CreateUserAccount(ctx context.Context, id, login, role, passwordHash string) error {
	login = strings.TrimSpace(strings.ToLower(login))
	if login == "" || role == "" {
		return errors.New("compte illisible")
	}
	_, err := s.pool.Exec(ctx, `
		INSERT INTO users (id, password_hash, login, role, onboarded) VALUES ($1, $2, $3, $4, true)`,
		id, passwordHash, login, role)
	return err
}

// InfrastructureOwner is the first account: servers, apps and secrets belong to it.
func (s *Store) InfrastructureOwner(ctx context.Context) (string, error) {
	var id string
	err := s.pool.QueryRow(ctx, `SELECT id FROM users ORDER BY created_at ASC LIMIT 1`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", ErrNotFound
	}
	return id, err
}

func (s *Store) UpdateUserLogin(ctx context.Context, userID, login string) error {
	login = strings.TrimSpace(strings.ToLower(login))
	if login == "" {
		return errors.New("identifiant vide")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE users SET login = $2 WHERE id = $1`, userID, login)
	if err != nil {
		if strings.Contains(err.Error(), "unique") || strings.Contains(err.Error(), "duplicate") {
			return errors.New("identifiant pris")
		}
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetUserTOTP(ctx context.Context, userID, secret string) error {
	tag, err := s.pool.Exec(ctx, `UPDATE users SET totp_secret = $2 WHERE id = $1`, userID, secret)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) DeleteUserAccount(ctx context.Context, id string) error {
	tag, err := s.pool.Exec(ctx, `DELETE FROM users WHERE id = $1`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}
