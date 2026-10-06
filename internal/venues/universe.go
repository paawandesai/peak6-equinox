package venues

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"

	"gopkg.in/yaml.v3"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/canonical"
)

// Universe is the set of markets one venue adapter fetches, per category.
// It is read from config/universe/<venue>.yaml (one file per venue).
type Universe struct {
	Venue      canonical.Venue
	Categories map[canonical.Category]Selector
}

// Selector names what to fetch for one category: Kalshi series tickers or
// Polymarket tag slugs. An adapter uses the field its venue understands.
type Selector struct {
	Series   []string `yaml:"series"`
	TagSlugs []string `yaml:"tag_slugs"`
}

// Universes maps each configured venue to its universe.
type Universes map[canonical.Venue]Universe

// For returns the universe for v; it is empty (no categories) if v has no file.
func (us Universes) For(v canonical.Venue) Universe {
	if u, ok := us[v]; ok {
		return u
	}
	return Universe{Venue: v, Categories: map[canonical.Category]Selector{}}
}

// universeFile is the on-disk shape of config/universe/<venue>.yaml.
type universeFile struct {
	Categories map[canonical.Category]Selector `yaml:"categories"`
}

// maxUniverseFileBytes caps a universe file; real files are a few hundred bytes.
const maxUniverseFileBytes = 1 << 20

// selectorPattern is the accepted form of a series ticker or tag slug.
var selectorPattern = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

// LoadUniverse reads <dir>/<venue>.yaml for every supported venue. A missing
// file is skipped (the venue fetches nothing). Unknown keys, unknown
// categories, malformed or duplicate entries are errors.
func LoadUniverse(dir string) (Universes, error) {
	out := Universes{}
	for _, v := range canonical.Venues() {
		path := filepath.Join(dir, string(v)+".yaml")
		u, err := loadUniverseFile(v, path)
		if errors.Is(err, os.ErrNotExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("loading universe %s: %w", path, err)
		}
		out[v] = u
	}
	if len(out) == 0 {
		return nil, fmt.Errorf("loading universe: no <venue>.yaml files in %s", dir)
	}
	return out, nil
}

func loadUniverseFile(v canonical.Venue, path string) (Universe, error) {
	f, err := os.Open(path) //nolint:gosec // path is UNIVERSE_DIR (operator config) joined with a fixed venue name
	if err != nil {
		return Universe{}, err
	}
	defer func() { _ = f.Close() }()
	data, err := io.ReadAll(io.LimitReader(f, maxUniverseFileBytes+1))
	if err != nil {
		return Universe{}, fmt.Errorf("reading: %w", err)
	}
	if len(data) > maxUniverseFileBytes {
		return Universe{}, fmt.Errorf("file exceeds %d bytes", maxUniverseFileBytes)
	}
	var uf universeFile
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&uf); err != nil {
		return Universe{}, fmt.Errorf("parsing yaml: %w", err)
	}
	if uf.Categories == nil {
		return Universe{}, errors.New("no categories")
	}
	for c, sel := range uf.Categories {
		if !c.Valid() {
			return Universe{}, fmt.Errorf("unknown category %q", c)
		}
		if err := checkSelectors(c, "series", sel.Series); err != nil {
			return Universe{}, err
		}
		if err := checkSelectors(c, "tag_slugs", sel.TagSlugs); err != nil {
			return Universe{}, err
		}
	}
	return Universe{Venue: v, Categories: uf.Categories}, nil
}

func checkSelectors(c canonical.Category, field string, values []string) error {
	seen := make(map[string]bool, len(values))
	for _, s := range values {
		if !selectorPattern.MatchString(s) {
			return fmt.Errorf("category %s: %s entry %q is not a valid identifier", c, field, s)
		}
		if seen[s] {
			return fmt.Errorf("category %s: duplicate %s entry %q", c, field, s)
		}
		seen[s] = true
	}
	return nil
}
