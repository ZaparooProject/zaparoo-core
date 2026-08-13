#!/bin/sh
set -eu

for command in unshare mount go; do
	if ! command -v "$command" >/dev/null 2>&1; then
		echo "$command is required" >&2
		exit 1
	fi
done

module_cache=$(go env GOMODCACHE)

# Command expands only inside the isolated namespace.
# shellcheck disable=SC2016
run_smoke_test='
	set -eu
	mount --make-rprivate /
	mount -t tmpfs tmpfs /media
	mkdir -p /media/fat
	cache_root=$(mktemp -d)
	export GOCACHE="$cache_root/go-build"
	export GOMODCACHE="$MODULE_CACHE"
	export GOPATH="$cache_root/gopath"
	if ZAPAROO_TEST_REAL_MOUNTS=1 go test -tags=integration ./pkg/platforms/mister/ \
		-run "^TestPrepareBackupRealBindMountSmoke$" -count=1 -v; then
		status=0
	else
		status=$?
	fi
	rm -rf "$cache_root"
	exit "$status"
'

run_privileged() {
	if ! command -v sudo >/dev/null 2>&1; then
		echo "sudo is required for privileged execution" >&2
		exit 1
	fi
	if ! sudo -n true 2>/dev/null; then
		echo "CI runner must provide passwordless sudo" >&2
		exit 1
	fi
	exec sudo -n env \
		"PATH=$PATH" \
		"HOME=$HOME" \
		"MODULE_CACHE=$module_cache" \
		unshare --mount sh -c "$run_smoke_test"
}

if [ "${CI:-}" = "true" ]; then
	run_privileged
fi
if command -v sudo >/dev/null 2>&1 && sudo -n true 2>/dev/null; then
	run_privileged
fi

exec env "MODULE_CACHE=$module_cache" \
	unshare --user --map-root-user --mount sh -c "$run_smoke_test"
