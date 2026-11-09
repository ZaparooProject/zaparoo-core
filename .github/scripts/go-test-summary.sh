#!/usr/bin/env bash
# Writes the failures from a saved `go test` log to the run's summary page and
# as error annotations, so a failed run can be read without its full log.
#
# Usage: go-test-summary.sh <go test output file> <heading>
set -euo pipefail

log="$1"
heading="$2"
summary="${GITHUB_STEP_SUMMARY:-/dev/stdout}"

if [ ! -s "$log" ]; then
	echo "### ${heading}: no test output was saved" >>"$summary"
	exit 0
fi

# Log lines from the code under test are JSON; they drown the assertion that
# failed, so they are left out of the summary.
failures="$(awk '
	function flush() {
		if (n > 0) { print "```"; for (i = 1; i <= n; i++) print buf[i]; print "```"; print "" }
		n = 0; inblock = 0
	}
	/^[[:space:]]*--- FAIL: / { flush(); inblock = 1; buf[++n] = $0; next }
	/^panic: / || /^fatal error: / { flush(); inblock = 1; buf[++n] = $0; next }
	/^# / { flush(); inblock = 1; buf[++n] = $0; next }
	inblock && (/^[[:space:]]*--- (PASS|SKIP): / || /^=== / || /^(ok|PASS|FAIL)([[:space:]]|$)/) { flush() }
	inblock {
		if ($0 ~ /^[[:space:]]*\{/) next
		if (n < 80) buf[++n] = $0
	}
	END { flush() }
' "$log")"

packages="$(grep -E '^FAIL[[:space:]]+[^[:space:]]' "$log" || true)"

{
	echo "### ${heading}"
	echo
	if [ -n "$packages" ]; then
		echo "Failed packages:"
		echo
		echo '```'
		echo "$packages"
		echo '```'
		echo
	fi
	if [ -n "$failures" ]; then
		echo "$failures"
	elif [ -z "$packages" ]; then
		echo "No test failures were found in the output. Check the step log."
	fi
} | head -c 900000 >>"$summary"

# grep finds nothing when only a build or panic failed; that is not an error.
{ grep -E '^[[:space:]]*--- FAIL: ' "$log" || true; } | sed -E 's/^[[:space:]]*--- FAIL: ([^ ]+).*/\1/' | sort -u | head -n 10 |
	while read -r name; do
		echo "::error title=${heading}::${name} failed"
	done
