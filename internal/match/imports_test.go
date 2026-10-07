package match

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

const modulePath = "labs.gauntletai.com/paawandesai/peak6-equinox"

// allowedImports are the only non-stdlib packages match may use (architecture.md).
var allowedImports = map[string]bool{
	modulePath + "/internal/canonical": true,
	modulePath + "/internal/facts":     true,
	"github.com/shopspring/decimal":    true,
}

// allowedTestImports may additionally appear in _test.go files: the
// property-testing library CLAUDE.md prescribes.
var allowedTestImports = map[string]bool{
	"pgregory.net/rapid":               true,
	"gopkg.in/yaml.v3":                 true, // the pair fixtures
	modulePath + "/internal/normalize": true, // the pair fixtures are normalised like the collector does
	modulePath + "/internal/match":     true, // the external test package
}

// TestImportsAreVenueAgnostic fails if any file in internal/match imports a
// package outside canonical, facts, shopspring/decimal or the standard library
// (M2-04 import discipline: no venues, no web).
func TestImportsAreVenueAgnostic(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("reading package directory: %v", err)
	}
	fset := token.NewFileSet()
	files := 0
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || filepath.Ext(name) != ".go" {
			continue
		}
		f, err := parser.ParseFile(fset, name, nil, parser.ImportsOnly)
		if err != nil {
			t.Fatalf("parsing %s: %v", name, err)
		}
		files++
		isTest := strings.HasSuffix(name, "_test.go")
		for _, imp := range f.Imports {
			path, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				t.Fatalf("%s: bad import %s", name, imp.Path.Value)
			}
			switch {
			case isStdlib(path), allowedImports[path], isTest && allowedTestImports[path]:
			default:
				t.Errorf("%s imports %q, which the matcher may not use (architecture.md)", name, path)
			}
		}
	}
	if files < 3 {
		t.Fatalf("parsed only %d files; expected the matcher sources", files)
	}
}

// isStdlib: a standard-library import path has no dot in its first element.
func isStdlib(path string) bool {
	first, _, _ := strings.Cut(path, "/")
	return !strings.Contains(first, ".")
}

func TestIsStdlib(t *testing.T) {
	for path, want := range map[string]bool{
		"fmt": true, "go/parser": true, "sort": true,
		"github.com/shopspring/decimal": false, modulePath + "/internal/venues/kalshi": false, "pgregory.net/rapid": false,
	} {
		if got := isStdlib(path); got != want {
			t.Errorf("isStdlib(%q) = %v, want %v", path, got, want)
		}
	}
}
