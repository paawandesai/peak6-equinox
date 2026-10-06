package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// QuoteRecord is a stored quote with its round and derived touch.
type QuoteRecord struct {
	canonical.Quote
	RoundID int64
	BestBid decimal.NullDecimal
	BestAsk decimal.NullDecimal
	Mid     decimal.NullDecimal // display only
}

func nullDec(d decimal.Decimal, ok bool) decimal.NullDecimal {
	return decimal.NullDecimal{Decimal: d, Valid: ok}
}

// InsertQuote validates q and stores it for the round with its best bid, best
// ask and mid. A second quote for the same round and instrument is an error.
func (s *Store) InsertQuote(ctx context.Context, roundID int64, q canonical.Quote) error {
	if err := q.Validate(); err != nil {
		return fmt.Errorf("inserting quote: %w", err)
	}
	bids, err := jsonParam(q.YesBids)
	if err != nil {
		return fmt.Errorf("inserting quote %q: %w", q.InstrumentID, err)
	}
	asks, err := jsonParam(q.YesAsks)
	if err != nil {
		return fmt.Errorf("inserting quote %q: %w", q.InstrumentID, err)
	}
	quality, err := jsonParam(q.Quality)
	if err != nil {
		return fmt.Errorf("inserting quote %q: %w", q.InstrumentID, err)
	}
	var venueTS *time.Time
	if q.VenueTS != nil {
		venueTS = utcPtr(*q.VenueTS)
	}
	_, err = s.pool.Exec(ctx, `
		INSERT INTO quotes (round_id, instrument_id, yes_bids, yes_asks, best_bid, best_ask, mid,
		                    fetched_at, venue_ts, quality)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)`,
		roundID, q.InstrumentID, bids, asks,
		nullDec(q.BestBid()), nullDec(q.BestAsk()), nullDec(q.Mid()),
		q.FetchedAt.UTC(), venueTS, quality)
	if err != nil {
		return fmt.Errorf("inserting quote %q: %w", q.InstrumentID, err)
	}
	return nil
}

const quoteCols = `round_id, instrument_id, yes_bids, yes_asks, best_bid, best_ask, mid, fetched_at, venue_ts, quality`

func scanQuote(row pgx.Row) (QuoteRecord, error) {
	var (
		r       QuoteRecord
		venueTS *time.Time
		quality canonical.Quality
	)
	err := row.Scan(&r.RoundID, &r.InstrumentID, &r.YesBids, &r.YesAsks, &r.BestBid, &r.BestAsk, &r.Mid,
		&r.FetchedAt, &venueTS, &quality)
	if err != nil {
		return QuoteRecord{}, err
	}
	r.FetchedAt = r.FetchedAt.UTC()
	if venueTS != nil {
		u := venueTS.UTC()
		r.VenueTS = &u
	}
	if len(quality) > 0 {
		r.Quality = quality
	}
	return r, nil
}

// GetQuote returns the quote for an instrument in a round.
func (s *Store) GetQuote(ctx context.Context, roundID int64, instrumentID string) (QuoteRecord, error) {
	if unstorable(instrumentID) {
		return QuoteRecord{}, fmt.Errorf("getting quote: %w", ErrNotFound)
	}
	r, err := scanQuote(s.pool.QueryRow(ctx,
		`SELECT `+quoteCols+` FROM quotes WHERE round_id = $1 AND instrument_id = $2`, roundID, instrumentID))
	if err != nil {
		return QuoteRecord{}, notFound(err, "getting quote")
	}
	return r, nil
}

// ListQuotes returns every quote in a round, ordered by instrument ID.
func (s *Store) ListQuotes(ctx context.Context, roundID int64) ([]QuoteRecord, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+quoteCols+` FROM quotes WHERE round_id = $1 ORDER BY instrument_id`, roundID)
	if err != nil {
		return nil, fmt.Errorf("listing quotes: %w", err)
	}
	defer rows.Close()
	var out []QuoteRecord
	for rows.Next() {
		r, err := scanQuote(rows)
		if err != nil {
			return nil, fmt.Errorf("listing quotes: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing quotes: %w", err)
	}
	return out, nil
}
