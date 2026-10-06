package store_test

import (
	"os"
	"testing"

	"labs.gauntletai.com/paawandesai/peak6-equinox/internal/store/testdb"
)

var tdb testdb.DB

func TestMain(m *testing.M) { os.Exit(tdb.Run(m)) }
