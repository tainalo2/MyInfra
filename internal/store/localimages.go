package store

import (
	"context"
	"errors"
	"io"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
)

// ErrImageInUse refuses to delete an image an application still runs.
var ErrImageInUse = errors.New("image utilisée")

// LocalImage is an image a person brought, kept by MyInfra.
type LocalImage struct {
	ID        string
	OwnerID   string
	Ref       string
	Source    string
	SourceURL string
	Size      int64
	Digest    string
	Platform  string
	Config    []byte
	CreatedAt time.Time
	// UsedBy names the applications that run it.
	UsedBy []string
}

const imageChunk = 4 << 20

// SaveLocalImage keeps an image and its archive, read from body piece by
// piece: an archive of a few gigabytes never sits in memory.
func (s *Store) SaveLocalImage(ctx context.Context, img LocalImage, body io.Reader) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	if _, err := tx.Exec(ctx, `
		INSERT INTO images (id, owner_id, ref, source, source_url, size, digest, platform, config)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)`,
		img.ID, img.OwnerID, img.Ref, img.Source, img.SourceURL, img.Size, img.Digest, img.Platform, img.Config); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrNameTaken
		}
		return err
	}
	buf := make([]byte, imageChunk)
	for seq := 0; ; seq++ {
		n, readErr := io.ReadFull(body, buf)
		if n > 0 {
			if _, err := tx.Exec(ctx, `INSERT INTO image_chunks (image_id, seq, data) VALUES ($1, $2, $3)`, img.ID, seq, buf[:n]); err != nil {
				return err
			}
		}
		if readErr == io.EOF || readErr == io.ErrUnexpectedEOF {
			break
		}
		if readErr != nil {
			return readErr
		}
	}
	return tx.Commit(ctx)
}

const localImageColumns = `
	i.id, i.owner_id, i.ref, i.source, i.source_url, i.size, i.digest, i.platform, i.config, i.created_at,
	COALESCE((
		SELECT array_agg(DISTINCT a.name ORDER BY a.name) FROM applications a
		LEFT JOIN application_services s ON s.application_id = a.id
		WHERE a.owner_id = i.owner_id AND (s.image = i.ref OR (a.source_kind = 'image' AND a.source = i.ref))
	), '{}')`

func scanLocalImage(row pgx.Row) (LocalImage, error) {
	var img LocalImage
	err := row.Scan(&img.ID, &img.OwnerID, &img.Ref, &img.Source, &img.SourceURL, &img.Size, &img.Digest, &img.Platform, &img.Config, &img.CreatedAt, &img.UsedBy)
	if errors.Is(err, pgx.ErrNoRows) {
		return LocalImage{}, ErrNotFound
	}
	return img, err
}

func (s *Store) LocalImages(ctx context.Context, ownerID string) ([]LocalImage, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+localImageColumns+` FROM images i WHERE i.owner_id = $1 ORDER BY i.created_at DESC`, ownerID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []LocalImage
	for rows.Next() {
		img, err := scanLocalImage(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, img)
	}
	return out, rows.Err()
}

func (s *Store) LocalImage(ctx context.Context, ownerID, id string) (LocalImage, error) {
	return scanLocalImage(s.pool.QueryRow(ctx, `SELECT `+localImageColumns+` FROM images i WHERE i.owner_id = $1 AND i.id = $2`, ownerID, id))
}

// LocalImageByRef finds an image by the name the servers know it by.
func (s *Store) LocalImageByRef(ctx context.Context, ref string) (LocalImage, error) {
	return scanLocalImage(s.pool.QueryRow(ctx, `SELECT `+localImageColumns+` FROM images i WHERE i.ref = $1`, ref))
}

func (s *Store) DeleteLocalImage(ctx context.Context, ownerID, id string) error {
	img, err := s.LocalImage(ctx, ownerID, id)
	if err != nil {
		return err
	}
	if len(img.UsedBy) > 0 {
		return ErrImageInUse
	}
	_, err = s.pool.Exec(ctx, `DELETE FROM images WHERE owner_id = $1 AND id = $2`, ownerID, id)
	return err
}

// OpenLocalImage reads the archive of an image back, one piece at a time.
func (s *Store) OpenLocalImage(ctx context.Context, id string) io.Reader {
	return &chunkReader{ctx: ctx, store: s, id: id}
}

type chunkReader struct {
	ctx   context.Context
	store *Store
	id    string
	seq   int
	buf   []byte
	done  bool
}

func (r *chunkReader) Read(p []byte) (int, error) {
	for len(r.buf) == 0 {
		if r.done {
			return 0, io.EOF
		}
		var data []byte
		err := r.store.pool.QueryRow(r.ctx, `SELECT data FROM image_chunks WHERE image_id = $1 AND seq = $2`, r.id, r.seq).Scan(&data)
		if errors.Is(err, pgx.ErrNoRows) {
			r.done = true
			continue
		}
		if err != nil {
			return 0, err
		}
		r.seq++
		r.buf = data
	}
	n := copy(p, r.buf)
	r.buf = r.buf[n:]
	return n, nil
}
