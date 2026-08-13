#!/bin/sh
set -eu

for command in sudo unshare mount go; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "$command is required" >&2
		exit 1
	fi
done

run_smoke_test='\
	set -eu
	mount --make-rprivate /
	mount -t tmpfs tmpfs /media
	mkdir -p /media/fat
	ZAPAROO_TEST_REAL_MOUNTS=1 go test ./pkg/platforms/mister/ \
		-run "^TestPrepareBackupRealBindMountSmoke$" -count=1 -v
'

if [ "${CI:-}" = "true" ] || sudo -n true 2>/dev/null; then
	if ! sudo -n true 2>/dev/null; then
		echo "CI runner must provide passwordless sudo" >&2
		exit 1
	fi
	exec sudo -n env \
		"PATH=$PATH" \
		"HOME=$HOME" \
		"GOCACHE=$(go env GOCACHE)" \
		"GOMODCACHE=$(go env GOMODCACHE)" \
		"GOPATH=$(go env GOPATH)" \
		unshare --mount sh -c "$run_smoke_test"
fi

exec unshare --user --map-root-user --mount sh -c "$run_smoke_test"
