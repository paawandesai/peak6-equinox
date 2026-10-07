//go:build e2e

package e2e

import (
	"os"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

// tdb is a fresh Postgres schema in DATABASE_URL_TEST, migrated by the
// helper before the tests and dropped afterwards (smoke step 3).
var tdb testdb.DB

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }
