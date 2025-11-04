#!/bin/bash
# Zaparoo Core wrapper script with architecture detection
# Detects system architecture and executes the correct binary

ARCH=$(uname -m)
BINARY="/userdata/system/zaparoo-${ARCH}"

if [ ! -f "$BINARY" ]; then
    echo "ERROR: Zaparoo Core binary for architecture '${ARCH}' not found at ${BINARY}" >&2
    echo "Available architectures: x86_64, aarch64, armv7l" >&2
    exit 1
fi

# Use exec to replace this process with the target binary
# This ensures os.Executable() in Go returns the real binary path, not this wrapper
exec "$BINARY" "$@"
