// Package migrations embeds the top-level numbered up-migration DDL files so
// the migrate command can run from a container image that ships only the binary.
// Optional rollback SQL should live under migrations/down/ (not embedded);
// cmd/migrate also excludes *_down.sql and *.down.sql.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
