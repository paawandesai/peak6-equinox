package store

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// UpsertRelationship validates r and stores it, replacing any earlier result
// for the same (round, a, b). It returns the row ID.
func (s *Store) UpsertRelationship(ctx context.Context, r canonical.Relationship) (int64, error) {
	if err := r.Validate(); err != nil {
		return 0, fmt.Errorf("upserting relationship: %w", err)
	}
	params := make([][]byte, 0, 3)
	for _, v := range []any{r.Diff, r.Vetoes, r.FactsUsed} {
		b, err := jsonParam(v)
		if err != nil {
			return 0, fmt.Errorf("upserting relationship: %w", err)
		}
		params = append(params, b)
	}
	var basis []byte // nil stores SQL NULL
	if r.BasisRisk != nil {
		b, err := jsonParam(r.BasisRisk)
		if err != nil {
			return 0, fmt.Errorf("upserting relationship: %w", err)
		}
		basis = b
	}
	var evidence []byte // nil stores SQL NULL
	if r.Evidence != nil {
		b, err := jsonParam(r.Evidence)
		if err != nil {
			return 0, fmt.Errorf("upserting relationship: %w", err)
		}
		evidence = b
	}
	var id int64
	err := s.pool.QueryRow(ctx, `
		INSERT INTO relationships (round_id, a_id, b_id, type, confidence, method, diff, vetoes, facts_used,
		                           basis_risk, evidence, explanation)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
		ON CONFLICT (round_id, a_id, b_id) DO UPDATE SET
		    type = EXCLUDED.type, confidence = EXCLUDED.confidence, method = EXCLUDED.method,
		    diff = EXCLUDED.diff, vetoes = EXCLUDED.vetoes, facts_used = EXCLUDED.facts_used,
		    basis_risk = EXCLUDED.basis_risk, evidence = EXCLUDED.evidence, explanation = EXCLUDED.explanation
		RETURNING id`,
		r.RoundID, r.AID, r.BID, r.Type, r.Confidence, r.Method, params[0], params[1], params[2],
		basis, evidence, r.Explanation).Scan(&id)
	if err != nil {
		return 0, fmt.Errorf("upserting relationship %s/%s: %w", r.AID, r.BID, err)
	}
	return id, nil
}

const relationshipCols = `id, round_id, a_id, b_id, type, confidence, method, diff, vetoes, facts_used,
	basis_risk, evidence, explanation`

func scanRelationship(row pgx.Row) (canonical.Relationship, error) {
	var r canonical.Relationship
	err := row.Scan(&r.ID, &r.RoundID, &r.AID, &r.BID, &r.Type, &r.Confidence, &r.Method,
		&r.Diff, &r.Vetoes, &r.FactsUsed, &r.BasisRisk, &r.Evidence, &r.Explanation)
	if err != nil {
		return canonical.Relationship{}, err
	}
	for i := range r.FactsUsed {
		r.FactsUsed[i].AsOf = r.FactsUsed[i].AsOf.UTC()
	}
	return r, nil
}

// GetRelationship returns one relationship by ID.
func (s *Store) GetRelationship(ctx context.Context, id int64) (canonical.Relationship, error) {
	r, err := scanRelationship(s.pool.QueryRow(ctx,
		`SELECT `+relationshipCols+` FROM relationships WHERE id = $1`, id))
	if err != nil {
		return canonical.Relationship{}, notFound(err, fmt.Sprintf("getting relationship %d", id))
	}
	return r, nil
}

// RelationshipFilter narrows ListRelationships. RoundID is required; other
// zero fields match everything.
type RelationshipFilter struct {
	RoundID int64
	Type    canonical.RelationshipType
	// InstrumentID keeps relationships where it is either A or B.
	InstrumentID string
	Limit        int // default 100, max 10000
}

// ListRelationships returns a round's relationships matching f, ordered by ID.
func (s *Store) ListRelationships(ctx context.Context, f RelationshipFilter) ([]canonical.Relationship, error) {
	if unstorable(string(f.Type), f.InstrumentID) {
		return nil, nil
	}
	rows, err := s.pool.Query(ctx, `SELECT `+relationshipCols+` FROM relationships
		WHERE round_id = $1 AND ($2 = '' OR type = $2) AND ($3 = '' OR a_id = $3 OR b_id = $3)
		ORDER BY id LIMIT $4`,
		f.RoundID, string(f.Type), f.InstrumentID, clampLimit(f.Limit, defaultListLimit, maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("listing relationships: %w", err)
	}
	defer rows.Close()
	var out []canonical.Relationship
	for rows.Next() {
		r, err := scanRelationship(rows)
		if err != nil {
			return nil, fmt.Errorf("listing relationships: %w", err)
		}
		out = append(out, r)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("listing relationships: %w", err)
	}
	return out, nil
}
