// Package migrations embeds the numbered DDL files so the migrate command can run
// from a container image that ships only the binary.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
