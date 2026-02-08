//go:build linux && embed_arcadedb

package arcadedb

import _ "embed"

//go:embed ArcadeDatabase.csv
var EmbeddedArcadeDB []byte
