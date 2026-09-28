//go:build (linux && embed_arcadedb) && !android

package arcadedb

import _ "embed"

//go:embed ArcadeDatabase.csv
var EmbeddedArcadeDB []byte
