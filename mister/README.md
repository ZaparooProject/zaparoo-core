# Zaparoo MiSTer library

Dependency-free MiSTer catalog and MGL generation shared by Zaparoo Core and standalone MiSTer tools.

This nested Go module is versioned independently from Zaparoo Core so consumers do not inherit Core's service, database, audio, hardware, or CGO dependency graph.

Packages:

- `catalog`: core IDs, folders, scan extensions, RBF paths, MGL slots, and groups
- `mgl`: pure MGL document generation

## How Core consumes this

Core builds this module straight from the working tree. The root `go.mod`
carries a `replace` pointing at `./mister`, so a change here takes effect in
Core on the same commit and there is no pin to bump.

The `require` line in the root `go.mod` is a placeholder. The `replace` means it
is never fetched during a Core build; it exists so the module is named. Point it
at a real `mister/vX.Y.Z` once one is tagged, for anyone who imports Core as a
library and also compiles `pkg/platforms/mister`.

Without the `replace` this drifts silently: Core would compile whatever version
the proxy last served while `task test` runs this module's own tests against the
tree, so both suites pass while the two copies disagree.

## Releasing

This module is versioned separately from Core. A `vX.Y.Z` release tag names only
the root module, so cutting a Core release does **not** publish this one. It
needs its own directory-prefixed tag:

```
task mister:tag-module -- v0.1.0
git push origin mister/v0.1.0
```

`task mister:module-tag-status` reports whether the directory has changed since
its last tag, and the release workflow raises a warning when a stable Core
release ships changes here that have not been tagged.

While the module is on `v0.x.y` the API is free to move between tags.

## License

GPL-3.0-or-later, the same as Zaparoo Core. The module zip carries the
repository's `LICENSE`, and every file here declares
`SPDX-License-Identifier: GPL-3.0-or-later`. Importing this module puts your own
program under the GPL too.
