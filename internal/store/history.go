package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/shopspring/decimal"
)

// ListRoundsSince returns every round started at or after since, newest
// first, capped at limit (default 400, max 10000). It feeds the collector
// success rate on the admin page (NFR-05), so it includes running rounds.
func (s *Store) ListRoundsSince(ctx context.Context, since time.Time, limit int) ([]Round, error) {
	rows, err := s.pool.Query(ctx,
		`SELECT `+roundCols+` FROM fetch_rounds WHERE started_at >= $1 ORDER BY id DESC LIMIT $2`,
		since.UTC(), clampLimit(limit, 400, maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("listing rounds since %s: %w", since.UTC().Format(time.RFC3339), err)
	}
	defer rows.Close()
	var out []Round
	for rows.Next() {
		r, err := scanRound(rows)
		if err != nil {
			return nil, fmt.Errorf("listing rounds since %s: %w", since.UTC().Format(time.RFC3339), err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing rounds since %s: %w", since.UTC().Format(time.RFC3339), err)
	}
	return out, nil
}

// QuotePoint is one instrument's touch in one round: the projection of a
// quotes row that a price-history chart needs (FR-25). Books are not loaded.
type QuotePoint struct {
	RoundID      int64
	InstrumentID string
	BestBid      decimal.NullDecimal
	BestAsk      decimal.NullDecimal
	Mid          decimal.NullDecimal
	FetchedAt    time.Time
}

// maxHistoryInstruments bounds one QuoteHistory call to a pair.
const maxHistoryInstruments = 2

// QuoteHistory returns the most recent limit touches (default 300, max 10000)
// for up to two instruments across every fetch round, oldest first. IDs that
// cannot be stored match nothing.
func (s *Store) QuoteHistory(ctx context.Context, instrumentIDs []string, limit int) ([]QuotePoint, error) {
	if len(instrumentIDs) == 0 || len(instrumentIDs) > maxHistoryInstruments || unstorable(instrumentIDs...) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `
		SELECT round_id, instrument_id, best_bid, best_ask, mid, fetched_at
		FROM (
			SELECT round_id, instrument_id, best_bid, best_ask, mid, fetched_at
			FROM quotes WHERE instrument_id = ANY($1)
			ORDER BY fetched_at DESC, round_id DESC LIMIT $2
		) recent
		ORDER BY fetched_at ASC, round_id ASC, instrument_id ASC`,
		instrumentIDs, clampLimit(limit, 300, maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("loading quote history: %w", err)
	}
	defer rows.Close()
	var out []QuotePoint
	for rows.Next() {
		p, err := scanQuotePoint(rows)
		if err != nil {
			return nil, fmt.Errorf("loading quote history: %w", err)
		}
		out = append(out, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("loading quote history: %w", err)
	}
	return out, nil
}

func scanQuotePoint(row pgx.Row) (QuotePoint, error) {
	var p QuotePoint
	if err := row.Scan(&p.RoundID, &p.InstrumentID, &p.BestBid, &p.BestAsk, &p.Mid, &p.FetchedAt); err != nil {
		return QuotePoint{}, err
	}
	p.FetchedAt = p.FetchedAt.UTC()
	return p, nil
}
