package venues

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

type fakeAdapter struct{ venue canonical.Venue }

func (f fakeAdapter) Venue() canonical.Venue { return f.venue }
func (fakeAdapter) FetchInstruments(context.Context, Universe) ([]canonical.Instrument, []RawResponse, error) {
	return nil, nil, nil
}
func (fakeAdapter) FetchQuotes(context.Context, []canonical.Instrument) ([]canonical.Quote, []RawResponse, error) {
	return nil, nil, nil
}
func (fakeAdapter) FetchSettled(context.Context, Universe, time.Time) ([]canonical.Settled, []RawResponse, error) {
	return nil, nil, nil
}

func TestRegistry(t *testing.T) {
	r, err := NewRegistry(fakeAdapter{canonical.VenuePolymarket}, fakeAdapter{canonical.VenueKalshi})
	if err != nil {
		t.Fatalf("NewRegistry: %v", err)
	}
	if r.Len() != 2 {
		t.Fatalf("Len = %d", r.Len())
	}
	got := r.Adapters()
	if got[0].Venue() != canonical.VenueKalshi || got[1].Venue() != canonical.VenuePolymarket {
		t.Errorf("Adapters not sorted by venue: %v, %v", got[0].Venue(), got[1].Venue())
	}
	if a, ok := r.Get(canonical.VenueKalshi); !ok || a.Venue() != canonical.VenueKalshi {
		t.Error("Get(kalshi) failed")
	}
	if _, ok := r.Get("nyse"); ok {
		t.Error("Get(nyse) found an adapter")
	}
	if err := r.Register(fakeAdapter{canonical.VenueKalshi}); err == nil || !strings.Contains(err.Error(), "registered twice") {
		t.Errorf("duplicate Register = %v", err)
	}
	if err := r.Register(nil); err == nil {
		t.Error("nil adapter accepted")
	}
	if _, err := NewRegistry(fakeAdapter{"nyse"}); err == nil || !strings.Contains(err.Error(), "unsupported venue") {
		t.Errorf("unsupported venue: %v", err)
	}
}

func repoUniverseDir(t *testing.T) string {
	t.Helper()
	return filepath.Join("..", "..", "config", "universe")
}

// TestLoadUniverseRepoFiles pins the M1 universe in config/universe/.
func TestLoadUniverseRepoFiles(t *testing.T) {
	us, err := LoadUniverse(repoUniverseDir(t))
	if err != nil {
		t.Fatalf("LoadUniverse: %v", err)
	}
	k := us.For(canonical.VenueKalshi)
	wantK := map[canonical.Category][]string{
		canonical.CategoryCrypto: {"KXBTCD", "KXETHD"}, canonical.CategoryFed: {"KXFED"}, canonical.CategoryPolitics: nil,
	}
	for c, want := range wantK {
		sel, ok := k.Categories[c]
		if !ok {
			t.Errorf("kalshi: category %s missing", c)
		}
		if len(sel.Series) != len(want) || (len(want) > 0 && !reflect.DeepEqual(sel.Series, want)) {
			t.Errorf("kalshi %s series = %v, want %v", c, sel.Series, want)
		}
		if len(sel.TagSlugs) != 0 {
			t.Errorf("kalshi %s has tag_slugs %v", c, sel.TagSlugs)
		}
	}
	p := us.For(canonical.VenuePolymarket)
	wantP := map[canonical.Category][]string{
		canonical.CategoryCrypto: {"crypto"}, canonical.CategoryFed: {"fed-rates"}, canonical.CategoryPolitics: nil,
	}
	for c, want := range wantP {
		sel, ok := p.Categories[c]
		if !ok {
			t.Errorf("polymarket: category %s missing", c)
		}
		if len(sel.TagSlugs) != len(want) || (len(want) > 0 && !reflect.DeepEqual(sel.TagSlugs, want)) {
			t.Errorf("polymarket %s tag_slugs = %v, want %v", c, sel.TagSlugs, want)
		}
	}
	for _, v := range canonical.Venues() {
		raw, err := os.ReadFile(filepath.Join(repoUniverseDir(t), string(v)+".yaml"))
		if err != nil {
			t.Fatal(err)
		}
		ticket := map[canonical.Venue]string{canonical.VenueKalshi: "M1-04", canonical.VenuePolymarket: "M1-05"}[v]
		if !strings.Contains(string(raw), "verified by "+ticket) {
			t.Errorf("%s.yaml lacks the 'verified by %s' comment", v, ticket)
		}
	}
}

func writeUniverse(t *testing.T, files map[string]string) string {
	t.Helper()
	dir := t.TempDir()
	for name, body := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return dir
}

func TestLoadUniverseErrors(t *testing.T) {
	cases := map[string]struct {
		body, wantErr string
	}{
		"unknown key":      {"categories:\n  crypto:\n    tickers: [A]\n", "field tickers not found"},
		"unknown top key":  {"venue: kalshi\ncategories: {}\n", "field venue not found"},
		"unknown category": {"categories:\n  sports:\n    series: [A]\n", `unknown category "sports"`},
		"bad entry":        {"categories:\n  crypto:\n    series: [\"'; DROP TABLE x\"]\n", "not a valid identifier"},
		"empty entry":      {"categories:\n  crypto:\n    tag_slugs: [\"\"]\n", "not a valid identifier"},
		"duplicate":        {"categories:\n  fed:\n    series: [KXFED, KXFED]\n", "duplicate series"},
		"no categories":    {"# empty\n{}\n", "no categories"},
		"not yaml":         {"categories: [unclosed\n", "parsing yaml"},
		"empty document":   {"", "parsing yaml"},
		"wrong type":       {"categories:\n  crypto: 5\n", "parsing yaml"},
		"oversize":         {"# " + strings.Repeat("x", maxUniverseFileBytes) + "\ncategories: {}\n", "exceeds"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			dir := writeUniverse(t, map[string]string{"kalshi.yaml": tc.body})
			_, err := LoadUniverse(dir)
			if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
				t.Fatalf("LoadUniverse = %v, want error containing %q", err, tc.wantErr)
			}
		})
	}
}

func TestLoadUniverseMissingFiles(t *testing.T) {
	dir := writeUniverse(t, map[string]string{"polymarket.yaml": "categories:\n  crypto:\n    tag_slugs: [crypto]\n"})
	us, err := LoadUniverse(dir)
	if err != nil {
		t.Fatalf("LoadUniverse: %v", err)
	}
	if _, ok := us[canonical.VenueKalshi]; ok {
		t.Error("kalshi present without a file")
	}
	if k := us.For(canonical.VenueKalshi); k.Venue != canonical.VenueKalshi || len(k.Categories) != 0 {
		t.Errorf("For(kalshi) = %+v, want empty universe", k)
	}
	if _, err := LoadUniverse(t.TempDir()); err == nil || !strings.Contains(err.Error(), "no <venue>.yaml files") {
		t.Errorf("empty dir: %v", err)
	}
	unreadable := writeUniverse(t, nil)
	if err := os.Mkdir(filepath.Join(unreadable, "kalshi.yaml"), 0o700); err != nil {
		t.Fatal(err)
	}
	if _, err := LoadUniverse(unreadable); err == nil {
		t.Error("directory named kalshi.yaml accepted")
	}
}

func TestFixtureFileName(t *testing.T) {
	a := FixtureFileName("/events?series_ticker=KXBTCD&status=open&with_nested_markets=true")
	if !strings.HasPrefix(a, "events_series_ticker-KXBTCD_status-open_with_nested_markets-true.") || !strings.HasSuffix(a, ".json") {
		t.Errorf("name = %q", a)
	}
	if b := FixtureFileName("/events?series_ticker=KXBTCD&status=open&with_nested_markets=true&cursor=abc"); b == a {
		t.Error("distinct endpoints collide")
	}
	if FixtureFileName("clob/book?token_id=1") == FixtureFileName("clob/book?token_id=2") {
		t.Error("token ids collide")
	}
	if got := FixtureFileName("/"); !strings.HasPrefix(got, "root.") {
		t.Errorf("root name = %q", got)
	}
	long := FixtureFileName("/markets/" + strings.Repeat("A", 500) + "/../../etc/passwd")
	if len(long) > maxFixtureStem+20 || strings.Contains(long, "/") {
		t.Errorf("long or unsafe name %q", long)
	}
	if strings.Contains(FixtureFileName("../../etc/passwd"), "/") {
		t.Error("path separator kept")
	}
}
