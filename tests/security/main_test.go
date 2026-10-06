package security

import (
	"os"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

// tdb is this package's Postgres schema (internal/store/testdb). Tests that
// need the database call tdb.Pool(t); the rest run without one.
var tdb testdb.DB

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }
