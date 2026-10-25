package store

import (
	"context"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"
)

// LibraryRelease is a release of the library as it was downloaded: it is
// checked again each time it is read.
type LibraryRelease struct {
	Repo      string
	Release   string
	Archive   []byte
	Sums      []byte
	Signature []byte
	FetchedAt time.Time
}

func (s *Store) LibraryRelease(ctx context.Context, repo string) (LibraryRelease, error) {
	row := LibraryRelease{Repo: repo}
	err := s.pool.QueryRow(ctx, `
		SELECT release, archive, sums, signature, fetched_at FROM library_cache WHERE repo = $1`, repo).
		Scan(&row.Release, &row.Archive, &row.Sums, &row.Signature, &row.FetchedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return LibraryRelease{}, ErrNotFound
	}
	return row, err
}

// LibraryReleaseName is the release kept for a repository, "" without one:
// a copy reads it often, to follow the one that fetches.
func (s *Store) LibraryReleaseName(ctx context.Context, repo string) (string, error) {
	var release string
	err := s.pool.QueryRow(ctx, `SELECT release FROM library_cache WHERE repo = $1`, repo).Scan(&release)
	if errors.Is(err, pgx.ErrNoRows) {
		return "", nil
	}
	return release, err
}

func (s *Store) SaveLibraryRelease(ctx context.Context, row LibraryRelease) error {
	_, err := s.pool.Exec(ctx, `
		INSERT INTO library_cache (repo, release, archive, sums, signature, fetched_at) VALUES ($1, $2, $3, $4, $5, now())
		ON CONFLICT (repo) DO UPDATE SET release = EXCLUDED.release, archive = EXCLUDED.archive,
			sums = EXCLUDED.sums, signature = EXCLUDED.signature, fetched_at = now()`,
		row.Repo, row.Release, row.Archive, row.Sums, row.Signature)
	return err
}

// SetPackageLibrary records where a package comes from in the library.
func (s *Store) SetPackageLibrary(ctx context.Context, ownerID, id string, origin []byte) error {
	return setLibrary(ctx, s, "packages", ownerID, id, origin)
}

// SetTemplateLibrary records where a template comes from in the library.
func (s *Store) SetTemplateLibrary(ctx context.Context, ownerID, id string, origin []byte) error {
	return setLibrary(ctx, s, "templates", ownerID, id, origin)
}

func setLibrary(ctx context.Context, s *Store, table, ownerID, id string, origin []byte) error {
	if len(origin) == 0 {
		origin = []byte("{}")
	}
	tag, err := s.pool.Exec(ctx, `UPDATE `+table+` SET library = $3 WHERE id = $1 AND owner_id = $2`, id, ownerID, string(origin))
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		return ErrNotFound
	}
	return nil
}

// LibraryCopy is a package or a template a person took from the library.
type LibraryCopy struct {
	Kind    string
	ID      string
	OwnerID string
	Name    string
	Body    []byte
	Library []byte
}

// LibraryCopies lists the packages and templates of every person that came
// from the library, to offer their newer versions.
func (s *Store) LibraryCopies(ctx context.Context) ([]LibraryCopy, error) {
	rows, err := s.pool.Query(ctx, `
		SELECT 'package', id, owner_id, name, body, library FROM packages WHERE library <> '{}'::jsonb
		UNION ALL
		SELECT 'template', id, owner_id, name, body, library FROM templates WHERE library <> '{}'::jsonb`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LibraryCopy
	for rows.Next() {
		var row LibraryCopy
		if err := rows.Scan(&row.Kind, &row.ID, &row.OwnerID, &row.Name, &row.Body, &row.Library); err != nil {
			return nil, err
		}
		out = append(out, row)
	}
	return out, rows.Err()
}
