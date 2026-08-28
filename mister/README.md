# Zaparoo MiSTer library

Dependency-free MiSTer catalog and MGL generation shared by Zaparoo Core and standalone MiSTer tools.

This nested Go module is versioned independently from Zaparoo Core so consumers do not inherit Core's service, database, audio, hardware, or CGO dependency graph.

Packages:

- `catalog`: core IDs, folders, scan extensions, RBF paths, MGL slots, and groups
- `mgl`: pure MGL document generation
