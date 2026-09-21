// Package migrations embeds the SQL schema migrations so the connector
// binary is self-contained: the schema a build expects always travels with
// it, and startup applies whatever is pending (see internal/storage).
package migrations

import "embed"

// FS holds every *.sql migration, applied in filename order.
//
//go:embed *.sql
var FS embed.FS
