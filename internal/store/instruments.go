package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// InstrumentRecord is a stored instrument with the rounds it was first and
// last seen in.
type InstrumentRecord struct {
	canonical.Instrument
	FirstSeenRound int64
	LastSeenRound  int64
}

// UpsertInstrument validates in and inserts it, or updates the stored row and
// sets last_seen_round when it already exists.
func (s *Store) UpsertInstrument(ctx context.Context, roundID int64, in canonical.Instrument) error {
	if err := in.Validate(); err != nil {
		return fmt.Errorf("upserting instrument: %w", err)
	}
	terms, err := jsonParam(in.Terms)
	if err != nil {
		return fmt.Errorf("upserting instrument %q: %w", in.ID, err)
	}
	fee, err := jsonParam(in.FeeSchedule)
	if err != nil {
		return fmt.Errorf("upserting instrument %q: %w", in.ID, err)
	}
	quality, err := jsonParam(in.Quality)
	if err != nil {
		return fmt.Errorf("upserting instrument %q: %w", in.ID, err)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO instruments (id, venue, venue_market_id, venue_event_id, category, title, rules_text,
		                         terms, polarity, close_time, expiration_time, status, fee_schedule, quality,
		                         first_seen_round, last_seen_round)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $15)
		ON CONFLICT (id) DO UPDATE SET
		    venue_market_id = EXCLUDED.venue_market_id, venue_event_id = EXCLUDED.venue_event_id,
		    category = EXCLUDED.category, title = EXCLUDED.title, rules_text = EXCLUDED.rules_text,
		    terms = EXCLUDED.terms, polarity = EXCLUDED.polarity, close_time = EXCLUDED.close_time,
		    expiration_time = EXCLUDED.expiration_time, status = EXCLUDED.status,
		    fee_schedule = EXCLUDED.fee_schedule, quality = EXCLUDED.quality,
		    last_seen_round = GREATEST(instruments.last_seen_round, EXCLUDED.last_seen_round)`,
		in.ID, in.Venue, in.VenueMarketID, in.VenueEventID, in.Category, in.Title, in.RulesText,
		terms, in.Terms.Polarity, utcPtr(in.CloseTime), utcPtr(in.ExpirationTime), in.Status, fee, quality,
		roundID)
	if err != nil {
		return fmt.Errorf("upserting instrument %q: %w", in.ID, err)
	}
	return nil
}

const instrumentCols = `id, venue, venue_market_id, venue_event_id, category, title, rules_text, terms,
	close_time, expiration_time, status, fee_schedule, quality, first_seen_round, last_seen_round`

func scanInstrument(row pgx.Row) (InstrumentRecord, error) {
	var (
		r               InstrumentRecord
		closeT, expiryT *time.Time
		quality         canonical.Quality
	)
	err := row.Scan(&r.ID, &r.Venue, &r.VenueMarketID, &r.VenueEventID, &r.Category, &r.Title, &r.RulesText,
		&r.Terms, &closeT, &expiryT, &r.Status, &r.FeeSchedule, &quality, &r.FirstSeenRound, &r.LastSeenRound)
	if err != nil {
		return InstrumentRecord{}, err
	}
	r.CloseTime, r.ExpirationTime = fromPtr(closeT), fromPtr(expiryT)
	if r.Terms.Deadline != nil {
		u := r.Terms.Deadline.UTC()
		r.Terms.Deadline = &u
	}
	if len(quality) > 0 {
		r.Quality = quality
	}
	return r, nil
}

// GetInstrument returns one instrument by ID.
func (s *Store) GetInstrument(ctx context.Context, id string) (InstrumentRecord, error) {
	if unstorable(id) {
		return InstrumentRecord{}, fmt.Errorf("getting instrument: %w", ErrNotFound)
	}
	r, err := scanInstrument(s.pool.QueryRow(ctx, `SELECT `+instrumentCols+` FROM instruments WHERE id = $1`, id))
	if err != nil {
		return InstrumentRecord{}, notFound(err, "getting instrument")
	}
	return r, nil
}

// InstrumentFilter narrows ListInstruments. Zero fields match everything.
type InstrumentFilter struct {
	Venue    canonical.Venue
	Category canonical.Category
	// SeenInRound keeps instruments whose last_seen_round equals this round.
	SeenInRound int64
	Limit       int // default 100, max 10000
}

// ListInstruments returns instruments matching f, ordered by ID.
func (s *Store) ListInstruments(ctx context.Context, f InstrumentFilter) ([]InstrumentRecord, error) {
	if unstorable(string(f.Venue), string(f.Category)) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+instrumentCols+` FROM instruments
		WHERE ($1 = '' OR venue = $1) AND ($2 = '' OR category = $2) AND ($3 = 0 OR last_seen_round = $3)
		ORDER BY id LIMIT $4`,
		string(f.Venue), string(f.Category), f.SeenInRound, clampLimit(f.Limit, defaultListLimit, maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("listing instruments: %w", err)
	}
	defer rows.Close()
	var out []InstrumentRecord
	for rows.Next() {
		r, err := scanInstrument(rows)
		if err != nil {
			return nil, fmt.Errorf("listing instruments: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing instruments: %w", err)
	}
	return out, nil
}

// GetInstruments returns the instruments with the given IDs, keyed by ID, in
// one query. IDs that do not exist are simply absent. Unstorable IDs (NUL
// bytes, invalid UTF-8) are dropped before the query (AB-08).
func (s *Store) GetInstruments(ctx context.Context, ids []string) (map[string]InstrumentRecord, error) {
	clean := make([]string, 0, len(ids))
	for _, id := range ids {
		if !unstorable(id) {
			clean = append(clean, id)
		}
	}
	out := make(map[string]InstrumentRecord, len(clean))
	if len(clean) == 0 {
		return out, nil
	}
	if len(clean) > maxListLimit {
		clean = clean[:maxListLimit]
	}
	rows, err := s.pool.Query(ctx, `SELECT `+instrumentCols+` FROM instruments WHERE id = ANY($1)`, clean)
	if err != nil {
		return nil, fmt.Errorf("getting instruments: %w", err)
	}
	defer rows.Close()
	for rows.Next() {
		r, err := scanInstrument(rows)
		if err != nil {
			return nil, fmt.Errorf("getting instruments: %w", err)
		}
		out[r.ID] = r
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("getting instruments: %w", err)
	}
	return out, nil
}
