// Package migrations embeds the schema's .sql files into the binary.
//
// go:embed only reaches the package's own directory, so the migrations are
// exposed from here and the `migrate` command consumes them. The point of
// embedding is not convenience: it makes it impossible to deploy a binary without
// its migrations, or to apply migrations on a server that do not belong to the
// version of the code running there.
package migrations

import "embed"

//go:embed *.sql
var FS embed.FS
