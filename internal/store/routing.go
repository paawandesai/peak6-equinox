package store

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// RoutingDecision is a routing_decisions row.
type RoutingDecision struct {
	ID        int64
	RoundID   int64
	Order     canonical.Order
	Decision  canonical.Decision
	CreatedAt time.Time
}

// InsertRoutingDecision validates d and stores it with its order, returning the row ID.
func (s *Store) InsertRoutingDecision(ctx context.Context, d canonical.Decision) (int64, error) {
	if err := d.Validate(); err != nil {
		return 0, fmt.Errorf("inserting routing decision: %w", err)
	}
	order, err := jsonParam(d.Order)
	if err != nil {
		return 0, fmt.Errorf("inserting routing decision: %w", err)
	}
	decision, err := jsonParam(d)
	if err != nil {
		return 0, fmt.Errorf("inserting routing decision: %w", err)
	}
	var id int64
	err = s.pool.QueryRow(ctx,
		`INSERT INTO routing_decisions (round_id, "order", decision, created_at) VALUES ($1, $2, $3, $4) RETURNING id`,
		d.RoundID, order, decision, d.DecidedAt.UTC()).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("inserting routing decision: %w", err)
	}
	return id, nil
}

const routingCols = `id, round_id, "order", decision, created_at`

func scanRouting(row pgx.Row) (RoutingDecision, error) {
	var r RoutingDecision
	if err := row.Scan(&r.ID, &r.RoundID, &r.Order, &r.Decision, &r.CreatedAt); err != nil {
		return RoutingDecision{}, err
	}
	r.CreatedAt = r.CreatedAt.UTC()
	r.Decision.DecidedAt = r.Decision.DecidedAt.UTC()
	return r, nil
}

// GetRoutingDecision returns one routing decision by ID.
func (s *Store) GetRoutingDecision(ctx context.Context, id int64) (RoutingDecision, error) {
	r, err := scanRouting(s.pool.QueryRow(ctx, `SELECT `+routingCols+` FROM routing_decisions WHERE id = $1`, id))
	if err != nil {
		return RoutingDecision{}, notFound(err, fmt.Sprintf("getting routing decision %d", id))
	}
	return r, nil
}

// ListRoutingDecisions returns the most recent routing decisions, newest first.
func (s *Store) ListRoutingDecisions(ctx context.Context, limit int) ([]RoutingDecision, error) {
	rows, err := s.pool.Query(ctx, `SELECT `+routingCols+` FROM routing_decisions ORDER BY id DESC LIMIT $1`,
		clampLimit(limit, defaultListLimit, maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("listing routing decisions: %w", err)
	}
	defer rows.Close()
	var out []RoutingDecision
	for rows.Next() {
		r, err := scanRouting(rows)
		if err != nil {
			return nil, fmt.Errorf("listing routing decisions: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing routing decisions: %w", err)
	}
	return out, nil
}
