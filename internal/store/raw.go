package store

import (
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"time"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// MaxRawBodyBytes caps a decompressed raw body on read, so a corrupt or
// hostile row cannot exhaust memory (gzip bomb).
const MaxRawBodyBytes = 64 << 20

// RawResponse is a raw_responses row. Body is uncompressed; the store gzips it.
type RawResponse struct {
	ID         int64
	RoundID    int64
	Venue      canonical.Venue
	Endpoint   string
	HTTPStatus int
	Body       []byte
	FetchedAt  time.Time
}

// InsertRawResponse gzips r.Body and stores it, returning the new row ID.
func (s *Store) InsertRawResponse(ctx context.Context, r RawResponse) (int64, error) {
	if !r.Venue.Valid() {
		return 0, fmt.Errorf("inserting raw response: venue %q is not supported", r.Venue)
	}
	gz, err := gzipBytes(r.Body)
	if err != nil {
		return 0, fmt.Errorf("inserting raw response: %w", err)
	}
	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO raw_responses (round_id, venue, endpoint, http_status, body_gz, fetched_at)
		 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
		r.RoundID, r.Venue, r.Endpoint, r.HTTPStatus, gz, r.FetchedAt.UTC()).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting raw response: %w", err)
	}
	return id, nil
}

const rawCols = `id, round_id, venue, endpoint, http_status, body_gz, fetched_at`

func scanRaw(row pgx.Row) (RawResponse, error) {
	var (
		r  RawResponse
		gz []byte
	)
	if err := row.Scan(&r.ID, &r.RoundID, &r.Venue, &r.Endpoint, &r.HTTPStatus, &gz, &r.FetchedAt); err != nil {
		return RawResponse{}, err
	}
	body, err := gunzipBytes(gz)
	if err != nil {
		return RawResponse{}, fmt.Errorf("raw response %d: %w", r.ID, err)
	}
	r.Body, r.FetchedAt = body, r.FetchedAt.UTC()
	return r, nil
}

// GetRawResponse returns one raw response with its decompressed body.
func (s *Store) GetRawResponse(ctx context.Context, id int64) (RawResponse, error) {
	r, err := scanRaw(s.pool.QueryRow(ctx, `SELECT `+rawCols+` FROM raw_responses WHERE id = $1`, id))
	if err != nil {
		return RawResponse{}, notFound(err, fmt.Sprintf("getting raw response %d", id))
	}
	return r, nil
}

// ListRawResponses returns a round's raw responses in insertion order.
func (s *Store) ListRawResponses(ctx context.Context, roundID int64) ([]RawResponse, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+rawCols+` FROM raw_responses WHERE round_id = $1 ORDER BY id`, roundID)
	if err != nil {
		return nil, fmt.Errorf("listing raw responses: %w", err)
	}
	defer rows.Close()
	var out []RawResponse
	for rows.Next() {
		r, err := scanRaw(rows)
		if err != nil {
			return nil, fmt.Errorf("listing raw responses: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing raw responses: %w", err)
	}
	return out, nil
}

func gzipBytes(b []byte) ([]byte, error) {
	var buf bytes.Buffer
	zw := gzip.NewWriter(&buf)
	if _, err := zw.Write(b); err != nil {
		return nil, fmt.Errorf("gzipping body: %w", err)
	}
	if err := zw.Close(); err != nil {
		return nil, fmt.Errorf("gzipping body: %w", err)
	}
	return buf.Bytes(), nil
}

func gunzipBytes(gz []byte) ([]byte, error) {
	zr, err := gzip.NewReader(bytes.NewReader(gz))
	if err != nil {
		return nil, fmt.Errorf("gunzipping body: %w", err)
	}
	defer func() { _ = zr.Close() }()
	body, err := io.ReadAll(io.LimitReader(zr, MaxRawBodyBytes+1))
	if err != nil {
		return nil, fmt.Errorf("gunzipping body: %w", err)
	}
	if len(body) > MaxRawBodyBytes {
		return nil, fmt.Errorf("gunzipped body exceeds %d bytes", MaxRawBodyBytes)
	}
	return body, nil
}
