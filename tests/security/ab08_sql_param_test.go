package security

import (
	"context"
	"errors"
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/shopspring/decimal"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store"
)

// hostileIDs are SQL-injection payloads passed through every store lookup.
var hostileIDs = []string{
	"'; DROP TABLE instruments; --",
	"'; DROP TABLE fetch_rounds CASCADE; --",
	"kalshi:X' OR '1'='1",
	"polymarket:1; DELETE FROM quotes",
	"\" OR \"\"=\"",
	"1) UNION SELECT table_name FROM information_schema.tables --",
	"kalshi:X'); TRUNCATE decision_log; --",
	"$1",
	"\\'; DROP TABLE relationships; --",
	"%' AND pg_sleep(5) --",
	"kalshi:\x00null",
	"kalshi:\xff\xfe invalid utf8",
	strings.Repeat("A'", 2000),
}

// sqlLeakMarkers are fragments that would mean a database error reached the caller.
var sqlLeakMarkers = []string{"SQLSTATE", "syntax error", "pq:", "pgx", "ERROR:", "relation", "column"}

var abTables = []string{
	"fetch_rounds", "raw_responses", "instruments", "quotes", "relationships",
	"llm_verdicts", "llm_usage", "routing_decisions", "decision_log",
}

func tableCounts(t *testing.T, pool *pgxpool.Pool) map[string]int64 {
	t.Helper()
	out := map[string]int64{}
	for _, tbl := range abTables {
		var n int64
		// Table names are the fixed literals above, not input.
		if err := pool.QueryRow(context.Background(), "SELECT count(*) FROM "+tbl).Scan(&n); err != nil {
			t.Fatalf("counting %s (table missing?): %v", tbl, err)
		}
		out[tbl] = n
	}
	return out
}

func checkNoLeak(t *testing.T, op, id string, err error) {
	t.Helper()
	if err == nil {
		return
	}
	if !errors.Is(err, store.ErrNotFound) {
		t.Errorf("%s(%.40q): unexpected error %v", op, id, err)
		return
	}
	for _, m := range sqlLeakMarkers {
		if strings.Contains(err.Error(), m) {
			t.Errorf("%s(%.40q): error leaks %q: %v", op, id, m, err)
		}
	}
}

func seedAB08(t *testing.T, s *store.Store) (roundID int64, instrumentID string) {
	t.Helper()
	ctx := context.Background()
	now := time.Date(2026, 10, 6, 17, 0, 0, 0, time.UTC)
	r, err := s.CreateRound(ctx, now)
	if err != nil {
		t.Fatal(err)
	}
	mk := func(id string, v canonical.Venue) canonical.Instrument {
		return canonical.Instrument{
			ID: id, Venue: v, VenueMarketID: strings.SplitN(id, ":", 2)[1], Category: canonical.CategoryCrypto,
			// Hostile text in stored fields must round-trip verbatim, never execute.
			Title: hostileIDs[0], RulesText: hostileIDs[5], Terms: canonical.UnknownTerms(),
			Status: canonical.StatusOpen, FeeSchedule: canonical.FeeSchedule{ID: canonical.FeeScheduleKalshiTakerV1},
		}
	}
	a, b := mk("kalshi:AB08-A", canonical.VenueKalshi), mk("polymarket:808", canonical.VenuePolymarket)
	for _, in := range []canonical.Instrument{a, b} {
		if err := s.UpsertInstrument(ctx, r.ID, in); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.InsertQuote(ctx, r.ID, canonical.Quote{InstrumentID: a.ID, FetchedAt: now}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertRelationship(ctx, canonical.Relationship{
		RoundID: r.ID, AID: a.ID, BID: b.ID, Type: canonical.RelRelated, Confidence: decimal.NewFromFloat(0.5),
		Method: canonical.MethodRules, Explanation: hostileIDs[1],
	}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.AppendLog(ctx, store.LogIngest, hostileIDs[6], hostileIDs[2], map[string]string{"x": hostileIDs[3]}, now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.InsertRawResponse(ctx, store.RawResponse{
		RoundID: r.ID, Venue: canonical.VenueKalshi, Endpoint: "/markets/" + hostileIDs[0], HTTPStatus: 200,
		Body: []byte(hostileIDs[4]), FetchedAt: now,
	}); err != nil {
		t.Fatal(err)
	}
	return r.ID, a.ID
}

// TestAB08_HostileIDsThroughStoreLookups passes injection payloads through
// every store lookup that takes caller text and asserts each one is treated as
// a plain value: no rows (or ErrNotFound), no leaked SQL errors, and every
// table intact with unchanged row counts.
func TestAB08_HostileIDsThroughStoreLookups(t *testing.T) {
	pool := tdb.Pool(t)
	tdb.Reset(t)
	s := store.New(pool)
	ctx := context.Background()
	roundID, realID := seedAB08(t, s)
	before := tableCounts(t, pool)

	for _, id := range hostileIDs {
		_, err := s.GetInstrument(ctx, id)
		if err == nil {
			t.Errorf("GetInstrument(%.40q) found a row", id)
		}
		checkNoLeak(t, "GetInstrument", id, err)

		_, err = s.GetQuote(ctx, roundID, id)
		if err == nil {
			t.Errorf("GetQuote(%.40q) found a row", id)
		}
		checkNoLeak(t, "GetQuote", id, err)

		lists := map[string]func() (int, error){
			"ListInstruments.Venue": func() (int, error) {
				r, err := s.ListInstruments(ctx, store.InstrumentFilter{Venue: canonical.Venue(id)})
				return len(r), err
			},
			"ListInstruments.Category": func() (int, error) {
				r, err := s.ListInstruments(ctx, store.InstrumentFilter{Category: canonical.Category(id)})
				return len(r), err
			},
			"ListRelationships.Type": func() (int, error) {
				r, err := s.ListRelationships(ctx, store.RelationshipFilter{RoundID: roundID, Type: canonical.RelationshipType(id)})
				return len(r), err
			},
			"ListRelationships.InstrumentID": func() (int, error) {
				r, err := s.ListRelationships(ctx, store.RelationshipFilter{RoundID: roundID, InstrumentID: id})
				return len(r), err
			},
			"ListLog.Kind": func() (int, error) {
				r, err := s.ListLog(ctx, store.LogFilter{Kind: store.LogKind(id)})
				return len(r), err
			},
		}
		for op, fn := range lists {
			n, err := fn()
			if err != nil {
				t.Errorf("%s(%.40q): %v", op, id, err)
			}
			if n != 0 {
				t.Errorf("%s(%.40q) matched %d rows; the payload was interpreted", op, id, n)
			}
		}
		// The decision-log ref is matched literally: only the entry whose ref is
		// exactly this payload comes back.
		entries, err := s.ListLog(ctx, store.LogFilter{Ref: id})
		if err != nil {
			t.Errorf("ListLog.Ref(%.40q): %v", id, err)
		}
		for _, e := range entries {
			if e.Ref != id {
				t.Errorf("ListLog.Ref(%.40q) returned ref %.40q", id, e.Ref)
			}
		}
		// IDs are also validated against the expected format before use (AB-08).
		if canonical.ValidateInstrumentID(id) == nil {
			t.Errorf("ValidateInstrumentID accepted %.40q", id)
		}
	}

	after := tableCounts(t, pool)
	for tbl, n := range before {
		if after[tbl] != n {
			t.Errorf("table %s row count changed: %d -> %d", tbl, n, after[tbl])
		}
	}

	// Hostile text stored in ordinary fields comes back verbatim.
	in, err := s.GetInstrument(ctx, realID)
	if err != nil || in.Title != hostileIDs[0] || in.RulesText != hostileIDs[5] {
		t.Errorf("hostile title round trip = %q, %q, %v", in.Title, in.RulesText, err)
	}
	if got, err := s.ListLog(ctx, store.LogFilter{Ref: hostileIDs[6]}); err != nil || len(got) != 1 || got[0].Summary != hostileIDs[2] {
		t.Errorf("hostile decision log round trip = %+v, %v", got, err)
	}
}

// TestAB08_StoreQueriesAreConstant parses internal/store and fails if any
// Query, QueryRow or Exec call builds its SQL from anything but string
// literals and constants, so caller values can only arrive as $n parameters.
func TestAB08_StoreQueriesAreConstant(t *testing.T) {
	paths, err := filepath.Glob(filepath.Join(repoRoot(t), "internal", "store", "*.go"))
	if err != nil {
		t.Fatal(err)
	}
	fset := token.NewFileSet()
	var files []*ast.File
	for _, p := range paths {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fset, p, nil, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", p, err)
		}
		files = append(files, f)
	}
	consts := packageConsts(files)
	calls := 0
	for _, f := range files {
		ast.Inspect(f, func(n ast.Node) bool {
			call, ok := n.(*ast.CallExpr)
			if !ok {
				return true
			}
			sel, ok := call.Fun.(*ast.SelectorExpr)
			if !ok || len(call.Args) < 2 {
				return true
			}
			switch sel.Sel.Name {
			case "Query", "QueryRow", "Exec":
			default:
				return true
			}
			calls++
			if !isConstSQL(call.Args[1], consts) {
				t.Errorf("%s: %s builds SQL from a non-constant expression", fset.Position(call.Pos()), sel.Sel.Name)
			}
			return true
		})
	}
	if calls < 20 {
		t.Errorf("found only %d SQL calls in internal/store; is the scan looking at the right files?", calls)
	}
}

// packageConsts collects the names of package-level constants.
func packageConsts(files []*ast.File) map[string]bool {
	out := map[string]bool{}
	for _, f := range files {
		for _, decl := range f.Decls {
			gd, ok := decl.(*ast.GenDecl)
			if !ok || gd.Tok != token.CONST {
				continue
			}
			for _, spec := range gd.Specs {
				for _, name := range spec.(*ast.ValueSpec).Names {
					out[name.Name] = true
				}
			}
		}
	}
	return out
}

func isConstSQL(e ast.Expr, consts map[string]bool) bool {
	switch v := e.(type) {
	case *ast.BasicLit:
		return v.Kind == token.STRING
	case *ast.Ident:
		return consts[v.Name]
	case *ast.ParenExpr:
		return isConstSQL(v.X, consts)
	case *ast.BinaryExpr:
		return v.Op == token.ADD && isConstSQL(v.X, consts) && isConstSQL(v.Y, consts)
	}
	return false
}
