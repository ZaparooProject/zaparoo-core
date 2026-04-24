Audit all direct dependencies for security, freshness, and maintenance status.

1. List all direct dependencies from go.mod
2. For each dependency, check:
   - Latest available version vs current version
   - Last commit date on the main repository
   - Any known CVEs not yet in the Go vulnerability database
   - Whether the module has been retracted or deprecated
3. Flag dependencies with:
   - No commits in 2+ years (potentially abandoned)
   - Major version behind latest
   - Known security advisories
4. Run `task vulncheck` and report any findings
5. Check `go mod tidy` for drift: `go mod tidy && git diff --exit-code go.mod go.sum`
6. Review indirect dependencies for any that should be pinned directly

Report findings as a table: dependency, current version, latest version, last activity, status (ok/warn/critical).
