Perform a security audit of zaparoo-core focusing on areas static analysis misses.

1. **Execute command allowlist audit**: Trace all paths to `exec.CommandContext` in `pkg/zapscript/utils.go`. Verify `IsExecuteAllowed()` gate is always hit. Confirm no bypass paths exist.

2. **Auth middleware coverage**: Trace the chi router setup in `pkg/api/server.go`. Verify no API endpoints skip authentication. Check WebSocket auth handler covers upgrade path.

3. **WebSocket message size limits**: Verify melody config in `pkg/api/server.go` prevents memory exhaustion from oversized messages. Check MaxMessageSize and WriteBufferSize settings.

4. **NDEF and CCID parser boundary analysis**: Run extended fuzzing on the NFC tag and CCID parsers:
   ```
   go test -run "^$" -fuzz=FuzzTextFromMemory -fuzztime=5m ./pkg/readers/shared/nfctag/
   go test -run "^$" -fuzz=FuzzReadType2 -fuzztime=5m ./pkg/readers/shared/nfctag/
   go test -run "^$" -fuzz=FuzzReadClassic -fuzztime=5m ./pkg/readers/shared/nfctag/
   go test -run "^$" -fuzz=FuzzATR -fuzztime=5m ./pkg/readers/ccid/
   go test -run "^$" -fuzz=FuzzConn -fuzztime=5m ./pkg/readers/ccid/proto/
   go test -run "^$" -fuzz=FuzzParseInterfaces -fuzztime=5m ./pkg/readers/ccid/proto/
   ```

5. **nolint:gosec directive audit**: Find all `nolint:gosec` directives and verify each is still justified. Flag any that suppress warnings about user-controlled input.

6. **Mapping regex safety**: Verify `pkg/database/userdb/mappings.go` only uses Go's `regexp` package (linear-time, safe from ReDoS). Confirm no switch to a different regex engine.

7. **Config path handling**: Check TOML config file path handling in `pkg/config/` can't be used to escape expected directories via path traversal.

8. Run `task vulncheck` for known CVEs.

Report each finding with: severity, affected file(s), evidence, and recommended fix.
