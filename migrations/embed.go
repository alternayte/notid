// Package migrations holds the goose files for the app tables.
package migrations

import (
	"embed"
	"fmt"
	"strconv"
	"strings"
)

//go:embed *.sql
var FS embed.FS

// Version is the newest app schema version, read from the goose file names.
func Version() (int64, error) {
	entries, err := FS.ReadDir(".")
	if err != nil {
		return 0, err
	}
	var newest int64
	for _, e := range entries {
		prefix, _, ok := strings.Cut(e.Name(), "_")
		if !ok {
			continue
		}
		v, err := strconv.ParseInt(prefix, 10, 64)
		if err != nil {
			return 0, fmt.Errorf("migration %s: %w", e.Name(), err)
		}
		newest = max(newest, v)
	}
	return newest, nil
}
