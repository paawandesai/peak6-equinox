// Package migrations embeds the goose SQL migrations (NNNN_name.sql) so the
// equinox binary can migrate without the source tree.
package migrations

import "embed"

// FS holds every migration file in this directory.
//
//go:embed *.sql
var FS embed.FS
