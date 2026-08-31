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

The `require` line in the root `go.mod` still names a published version. That
is only what external consumers of Zaparoo Core resolve; the `replace` means it
is never fetched during a Core build. Nothing needs to be done to it when this
module changes.

Without the `replace` this drifts silently: Core would compile whatever version
the proxy last served while `task test` runs this module's own tests against the
tree, so both suites pass while the two copies disagree.

External consumers of this module get it the normal way, by module path and
version.
