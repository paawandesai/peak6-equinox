package polymarket

import (
	"encoding/json"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/shopspring/decimal"
	"pgregory.net/rapid"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// TestDecodeStringArrayProperty: any string slice, JSON-encoded and then
// encoded again as a JSON string (Gamma's double encoding), decodes back to
// itself; the singly encoded form decodes the same way.
func TestDecodeStringArrayProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		want := rapid.SliceOfN(rapid.String(), 0, maxEncodedEntries).Draw(t, "values")
		inner, err := json.Marshal(want)
		if err != nil {
			t.Fatal(err)
		}
		outer, err := json.Marshal(string(inner))
		if err != nil {
			t.Fatal(err)
		}
		if len(outer) > maxEncodedArray {
			t.Skip("over the size cap")
		}
		for name, raw := range map[string][]byte{"double": outer, "single": inner} {
			got, err := decodeStringArray(raw)
			if err != nil {
				t.Fatalf("%s: decodeStringArray(%s) = %v", name, raw, err)
			}
			if len(want) == 0 && len(got) == 0 {
				continue
			}
			if !reflect.DeepEqual(got, want) {
				t.Fatalf("%s: got %q, want %q", name, got, want)
			}
		}
	})
}

// TestDecodeStringArrayNeverPanics: arbitrary bytes give a result or an error.
func TestDecodeStringArrayNeverPanics(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		raw := rapid.SliceOfN(rapid.Byte(), 0, 256).Draw(t, "raw")
		got, err := decodeStringArray(raw)
		if err == nil && len(got) > maxEncodedEntries {
			t.Fatalf("returned %d entries", len(got))
		}
	})
}

func TestDecodeStringArrayCases(t *testing.T) {
	cases := map[string]struct {
		raw     string
		want    []string
		wantErr bool
	}{
		"double":          {raw: `"[\"Yes\", \"No\"]"`, want: []string{"Yes", "No"}},
		"single":          {raw: `["Yes","No"]`, want: []string{"Yes", "No"}},
		"null":            {raw: `null`},
		"empty":           {raw: ``},
		"bad outer":       {raw: `"[\"Yes\"`, wantErr: true},
		"bad inner":       {raw: `"[Yes, No]"`, wantErr: true},
		"numbers":         {raw: `"[1, 2]"`, wantErr: true},
		"triple":          {raw: `"\"[\\\"Yes\\\"]\""`, wantErr: true},
		"object":          {raw: `{"a":1}`, wantErr: true},
		"too many":        {raw: `"[` + strings.Repeat(`\"a\",`, maxEncodedEntries) + `\"a\"]"`, wantErr: true},
		"too long":        {raw: `"` + strings.Repeat(" ", maxEncodedArray) + `"`, wantErr: true},
		"nested in inner": {raw: `"[[\"Yes\"]]"`, wantErr: true},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			got, err := decodeStringArray(json.RawMessage(tc.raw))
			if (err != nil) != tc.wantErr || (!tc.wantErr && !reflect.DeepEqual(got, tc.want)) {
				t.Fatalf("got %q, %v", got, err)
			}
		})
	}
}

// TestParsePriceProperty: every decimal string in [0,1] parses to itself and
// every value outside is rejected (NFR-08).
func TestParsePriceProperty(t *testing.T) {
	rapid.Check(t, func(t *rapid.T) {
		units := rapid.Int64Range(-2_000_000, 2_000_000).Draw(t, "units")
		scale := rapid.Int32Range(0, 6).Draw(t, "scale")
		d := decimal.New(units, -scale)
		got, err := parsePrice(d.StringFixed(scale))
		inRange := !d.IsNegative() && !d.GreaterThan(decimal.NewFromInt(1))
		if inRange != (err == nil) {
			t.Fatalf("parsePrice(%s) err = %v, in range = %v", d.StringFixed(scale), err, inRange)
		}
		if err == nil && !got.Equal(d) {
			t.Fatalf("parsePrice(%s) = %s", d, got)
		}
	})
}

func TestParseDecimalRejects(t *testing.T) {
	for _, s := range []string{"", " ", "NaN", "Inf", "-Inf", "1e400", "0.5e0", "abc", "0x1", strings.Repeat("1", 64), "1/2"} {
		if _, err := parseDecimal(s); err == nil {
			t.Errorf("parseDecimal(%q) accepted", s)
		}
	}
	if _, err := parseSize("0"); err == nil {
		t.Error("zero size accepted")
	}
	if _, err := parseSize("-1"); err == nil {
		t.Error("negative size accepted")
	}
	if d, err := parseSize("8837.58"); err != nil || d.String() != "8837.58" {
		t.Errorf("parseSize = %v, %v", d, err)
	}
}

func TestParseGammaTime(t *testing.T) {
	want := time.Date(2026, 3, 4, 19, 25, 43, 0, time.UTC)
	for _, s := range []string{"2026-03-04T19:25:43Z", "2026-03-04 19:25:43+00", "2026-03-04T14:25:43-05:00", "2026-03-04 19:25:43.000000+00"} {
		got, err := parseGammaTime(s)
		if err != nil || !got.Equal(want) || got.Location() != time.UTC {
			t.Errorf("parseGammaTime(%q) = %v, %v", s, got, err)
		}
	}
	if got, err := parseGammaTime("2026-03-04"); err != nil || !got.Equal(time.Date(2026, 3, 4, 0, 0, 0, 0, time.UTC)) {
		t.Errorf("date only = %v, %v", got, err)
	}
	if got, err := parseGammaTime(""); err != nil || !got.IsZero() {
		t.Errorf("empty = %v, %v", got, err)
	}
	for _, s := range []string{"tomorrow", "03/04/2026", strings.Repeat("9", 100)} {
		if _, err := parseGammaTime(s); err == nil {
			t.Errorf("parseGammaTime(%q) accepted", s)
		}
	}
}

func TestParseBookTimestamp(t *testing.T) {
	if got, err := parseBookTimestamp("1791322179740"); err != nil || got.UnixMilli() != 1791322179740 || got.Location() != time.UTC {
		t.Errorf("= %v, %v", got, err)
	}
	for _, s := range []string{"", "abc", "1.5", "-1", "1", "99999999999999999", "9223372036854775808"} {
		if _, err := parseBookTimestamp(s); err == nil {
			t.Errorf("parseBookTimestamp(%q) accepted", s)
		}
	}
}

func TestTruncate(t *testing.T) {
	if got := truncate("héllo", 2); got != "h…" {
		t.Errorf("truncate = %q", got)
	}
	if got := truncate("abc", 5); got != "abc" {
		t.Errorf("truncate = %q", got)
	}
}

// FuzzParseEvents: arbitrary bytes as a Gamma /events page never panic, and
// every instrument the adapter would build from them is valid.
func FuzzParseEvents(f *testing.F) {
	f.Add([]byte(`[]`))
	f.Add([]byte(`[` + event("e", market(nil)) + `]`))
	f.Add([]byte(`[{"slug":"x","markets":[{"question":"q","conditionId":"0x1","outcomes":"[\"Yes\",\"No\"]","clobTokenIds":"[\"1\",\"2\"]","outcomePrices":"[\"1\",\"0\"]","closed":true}]}]`))
	f.Add([]byte(`[{"slug":"x","markets":[{"outcomes":"[\"Yes\"","clobTokenIds":7}]}]`))
	f.Add([]byte(`{"error":"x"}`))
	if seeds, err := filepath.Glob(filepath.Join(fixtureDir, "gamma_events_*fed-rates*.json")); err == nil && len(seeds) > 0 {
		if b, err := os.ReadFile(seeds[0]); err == nil && len(b) < 2<<20 {
			f.Add(b)
		}
	}
	a, err := New(Options{GammaURL: "http://g", ClobURL: "http://c", Timeout: time.Second, MaxBodyBytes: 1, Logger: quietLogger()})
	if err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, body []byte) {
		events, _, err := parseEvents(body)
		if err != nil {
			return
		}
		for _, ev := range events {
			for _, in := range a.openInstruments(ev, canonical.CategoryFed) {
				if err := in.Validate(); err != nil {
					t.Fatalf("invalid instrument from fuzzed input: %v", err)
				}
			}
			for _, s := range a.settledInstruments(ev, canonical.CategoryFed, time.Time{}) {
				if err := s.Validate(); err != nil {
					t.Fatalf("invalid settled from fuzzed input: %v", err)
				}
			}
		}
	})
}

// FuzzMapBook: arbitrary CLOB book bodies never panic and always give a valid quote.
func FuzzMapBook(f *testing.F) {
	f.Add([]byte(book("1", `{"price":"0.4","size":"1"}`, `{"price":"0.6","size":"2"}`)))
	f.Add([]byte(`{"asset_id":"1","bids":[{"price":"NaN","size":"-1"}]}`))
	f.Add([]byte(`null`))
	f.Fuzz(func(t *testing.T, body []byte) {
		q := canonical.Quote{InstrumentID: "polymarket:1", FetchedAt: recordedAt}
		mapBook(&q, "1", body)
		if err := q.Validate(); err != nil {
			t.Fatalf("invalid quote from fuzzed input: %v", err)
		}
	})
}
