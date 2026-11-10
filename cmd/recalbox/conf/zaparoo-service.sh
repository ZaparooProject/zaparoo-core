#!/bin/sh
# Zaparoo Core supervisor, installed by "zaparoo -install". EmulationStation
# runs "(permanent)" user scripts once at startup and leaves them running.

ZAPAROO_BIN="@ZAPAROO_BIN@"
# The share is FAT mounted without exec bits, so run a copy from tmpfs and
# name the share binary in ZAPAROO_APP; updates replace that one.
RUN_DIR="${ZAPAROO_RUN_DIR:-/tmp/zaparoo-bin}"
RUN_BIN="$RUN_DIR/zaparoo-start"
export ZAPAROO_APP="$ZAPAROO_BIN"
PIDFILE="${ZAPAROO_SUPERVISOR_PIDFILE:-/tmp/zaparoo-supervisor.pid}"
RETRY_DELAY="${ZAPAROO_RETRY_DELAY:-5}"
# Core exits with this status when restarting cannot help.
EXIT_UNRECOVERABLE=78

if [ -f "$PIDFILE" ]; then
	old=$(cat "$PIDFILE" 2>/dev/null)
	case "$(tr '\000' ' ' < "/proc/$old/cmdline" 2>/dev/null)" in
	*zaparoo*) exit 0 ;;
	esac
fi
echo $$ > "$PIDFILE"

child=""
stop() {
	if [ -n "$child" ]; then
		kill "$child" 2>/dev/null
		wait "$child"
	fi
	rm -f "$PIDFILE"
	exit 0
}
trap stop TERM INT HUP

while :; do
	if ! { mkdir -p "$RUN_DIR" && cp -f "$ZAPAROO_BIN" "$RUN_BIN.new" &&
		chmod 755 "$RUN_BIN.new" && mv -f "$RUN_BIN.new" "$RUN_BIN"; }; then
		rm -f "$RUN_BIN.new"
		sleep "$RETRY_DELAY"
		continue
	fi
	"$RUN_BIN" -daemon >/dev/null 2>&1 &
	child=$!
	wait "$child"
	status=$?
	child=""
	case "$status" in
	0 | "$EXIT_UNRECOVERABLE") break ;;
	esac
	sleep "$RETRY_DELAY"
done
rm -f "$PIDFILE"
exit "$status"
