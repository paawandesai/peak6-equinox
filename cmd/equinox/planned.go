package main

// planned lists subcommands that a later ticket will add. Until then, invoking
// one prints "not implemented yet (ticket Mx-yy)" and exits 0, so Makefile
// targets such as `make migrate` work from M1-01 on. A registered subcommand
// always takes precedence, so later tickets just add their file.
var planned = map[string]string{
	"migrate":         "M1-03",
	"record-fixtures": "M1-03",
	"ingest":          "M1-06",
	"seed":            "M1-06",
	"fixture-server":  "M1-06",
}
