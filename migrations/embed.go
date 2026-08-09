package migrations

import "embed"

// Files embeds all migration SQL files so the binary is self-contained and
// migrations run automatically at startup without an external migration tool.
//
//go:embed *.sql
var Files embed.FS
