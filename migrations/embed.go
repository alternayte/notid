// Package migrations holds the goose files for the app tables.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
