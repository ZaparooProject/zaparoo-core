Perform a security audit of zaparoo-core focusing on areas static analysis misses.

1. **Execute command allowlist audit**: Trace all paths to `exec.CommandContext` in `pkg/zapscript/utils.go`. Verify `IsExecuteAllowed()` gate is always hit. Confirm no bypass paths exist.

2. **Auth middleware coverage**: Trace the chi router setup in `pkg/api/server.go`. Verify no API endpoints skip authentication. Check WebSocket auth handler covers upgrade path.

3. **WebSocket message size limits**: Verify melody config in `pkg/api/server.go` prevents memory exhaustion from oversized messages. Check MaxMessageSize and WriteBufferSize settings.

4. **NDEF parser boundary analysis**: Run extended fuzzing on the 5 fuzz functions in `pkg/readers/shared/ndef/parser_fuzz_test.go`:
   ```
   go test -run "^$" -fuzz=FuzzParseToText -fuzztime=5m ./pkg/readers/shared/ndef/
   go test -run "^$" -fuzz=FuzzValidateNDEFMessage -fuzztime=5m ./pkg/readers/shared/ndef/
   go test -run "^$" -fuzz=FuzzExtractTLVPayload -fuzztime=5m ./pkg/readers/shared/ndef/
   go test -run "^$" -fuzz=FuzzParseTextPayload -fuzztime=5m ./pkg/readers/shared/ndef/
   go test -run "^$" -fuzz=FuzzParseURIPayload -fuzztime=5m ./pkg/readers/shared/ndef/
   ```

5. **nolint:gosec directive audit**: Find all `nolint:gosec` directives and verify each is still justified. Flag any that suppress warnings about user-controlled input.

6. **Mapping regex safety**: Verify `pkg/database/userdb/mappings.go` only uses Go's `regexp` package (linear-time, safe from ReDoS). Confirm no switch to a different regex engine.

7. **Config path handling**: Check TOML config file path handling in `pkg/config/` can't be used to escape expected directories via path traversal.

8. Run `task vulncheck` for known CVEs.

Report each finding with: severity, affected file(s), evidence, and recommended fix.
