#!/bin/bash
set -euo pipefail

# Minimal ALSA library cross-compilation build script using Zig
# This script builds only the shared library - no utilities, plugins, or documentation

TARGETS_CSV=$1
HOSTS_CSV=$2

# Convert space-separated strings to arrays
IFS=' ' read -r -a TARGET_ARRAY <<< "$TARGETS_CSV"
IFS=' ' read -r -a HOST_ARRAY <<< "$HOSTS_CSV"

if [ ${#TARGET_ARRAY[@]} -ne ${#HOST_ARRAY[@]} ]; then
    echo "Error: Number of targets (${#TARGET_ARRAY[@]}) and hosts (${#HOST_ARRAY[@]}) do not match."
    exit 1
fi

echo "Building alsa-lib for ${#TARGET_ARRAY[@]} target(s): ${TARGETS_CSV}"

for i in "${!TARGET_ARRAY[@]}"; do
    TARGET="${TARGET_ARRAY[$i]}"
    HOST="${HOST_ARRAY[$i]}"
    PREFIX="/opt/sysroot/${TARGET}"

    echo "--- Building alsa-lib for ${TARGET} ($((i+1))/${#TARGET_ARRAY[@]}) ---"

    # Clean up from previous builds (ignore errors on first run)
    make clean || true

    mkdir -p "${PREFIX}"

    # Set up environment variables for cross-compilation with Zig
    export CC="zig cc -target ${TARGET}"
    export CXX="zig c++ -target ${TARGET}"
    export PKG_CONFIG_PATH="${PREFIX}/lib/pkgconfig"
    export CFLAGS="-I${PREFIX}/include"
    export LDFLAGS="-L${PREFIX}/lib"

    echo "  CC: ${CC}"
    echo "  CXX: ${CXX}"
    echo "  PREFIX: ${PREFIX}"
    echo "  PKG_CONFIG_PATH: ${PKG_CONFIG_PATH}"

    # Configure with minimal settings for cross-compilation
    # We only need the shared library - no utilities, no plugins, no docs
    if ! ./configure \
        --host="${HOST}" \
        --prefix="${PREFIX}" \
        --disable-python \
        --disable-static \
        --enable-shared \
        --with-pic=yes \
        --with-debug=no \
        --with-versioned=yes; then
        echo "=== Configure failed for alsa-lib (${TARGET}), showing config.log ==="
        if [ -f config.log ]; then
            cat config.log
        else
            echo "No config.log found"
        fi
        exit 1
    fi

    # Build and install
    make -j$(nproc)
    make install

    echo "  ✓ Successfully built alsa-lib for ${TARGET}"
    echo "  Libraries installed to: ${PREFIX}/lib"
    echo "  Headers installed to: ${PREFIX}/include"
    echo ""
done

echo "✓ Completed building alsa-lib for all targets"
