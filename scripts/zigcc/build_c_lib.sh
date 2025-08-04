#!/bin/bash
set -euo pipefail

# Cross-compilation build script for C libraries using Zig
# This script handles the repetitive configure→make→install pattern across multiple architectures

LIB_NAME=$1
PREFIX_ROOT=$2
EXTRA_CONFIGURE_ARGS=$3
TARGETS_CSV=$4
HOSTS_CSV=$5

# Convert space-separated strings to arrays
IFS=' ' read -r -a TARGET_ARRAY <<< "$TARGETS_CSV"
IFS=' ' read -r -a HOST_ARRAY <<< "$HOSTS_CSV"

if [ ${#TARGET_ARRAY[@]} -ne ${#HOST_ARRAY[@]} ]; then
    echo "Error: Number of targets (${#TARGET_ARRAY[@]}) and hosts (${#HOST_ARRAY[@]}) do not match."
    exit 1
fi

echo "Building ${LIB_NAME} for ${#TARGET_ARRAY[@]} target(s): ${TARGETS_CSV}"

for i in "${!TARGET_ARRAY[@]}"; do
    TARGET="${TARGET_ARRAY[$i]}"
    HOST="${HOST_ARRAY[$i]}"
    PREFIX="${PREFIX_ROOT}/${TARGET}"
    
    echo "--- Building ${LIB_NAME} for ${TARGET} (${i+1}/${#TARGET_ARRAY[@]}) ---"
    
    # Clean up from previous builds (ignore errors on first run)
    make clean || true 
    
    mkdir -p "${PREFIX}"
    
    # Set up environment variables for cross-compilation with Zig
    # For libnfc, we need to point to the libusb dependencies in /opt/deps
    if [ "${LIB_NAME}" = "libnfc" ]; then
        export PKG_CONFIG_PATH="/opt/deps/${TARGET}/lib/pkgconfig"
        export CFLAGS="-I/opt/deps/${TARGET}/include"
        export LDFLAGS="-L/opt/deps/${TARGET}/lib"
    else
        export PKG_CONFIG_PATH="${PREFIX}/lib/pkgconfig"
        export CFLAGS="-I${PREFIX}/include"
        export LDFLAGS="-L${PREFIX}/lib"
    fi
    
    export CC="zig cc -target ${TARGET}"
    export CXX="zig c++ -target ${TARGET}"
    
    echo "  CC: ${CC}"
    echo "  CXX: ${CXX}"
    echo "  PREFIX: ${PREFIX}"
    echo "  PKG_CONFIG_PATH: ${PKG_CONFIG_PATH}"

    # Configure with error handling
    if ! ./configure \
        --host="${HOST}" \
        --prefix="${PREFIX}" \
        --enable-static \
        --disable-shared \
        ${EXTRA_CONFIGURE_ARGS}; then
        echo "=== Configure failed for ${LIB_NAME} (${TARGET}), showing config.log ==="
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
    
    echo "  ✓ Successfully built ${LIB_NAME} for ${TARGET}"
    echo "  Libraries installed to: ${PREFIX}/lib"
    echo "  Headers installed to: ${PREFIX}/include"
    echo ""
done

echo "✓ Completed building ${LIB_NAME} for all targets"