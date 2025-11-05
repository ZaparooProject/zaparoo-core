FROM alpine:3.21 AS base

# Install base dependencies in single layer
RUN apk add --no-cache \
    ca-certificates \
    curl \
    git \
    build-base \
    autoconf \
    automake \
    libtool \
    pkgconfig \
    linux-headers \
    bash \
    eudev-dev \
    mingw-w64-gcc \
    alsa-lib-dev \
    xz && \
    # Clean up to reduce image size
    rm -rf /var/cache/apk/*

# Install Go
COPY --from=golang:1.24-alpine /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

# Install Zig
ENV ZIG_VERSION=0.14.0
RUN ARCH=$(uname -m | sed 's/arm64/aarch64/g') && \
    curl -L "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-${ARCH}-${ZIG_VERSION}.tar.xz" | \
    tar -C /usr/local/bin --strip-components=1 -xJ

# Install macOS SDK
RUN mkdir -p /opt/macosx-sdk && \
    curl -sSL -o /tmp/macosx-sdk.tar.xz \
    "https://github.com/phracker/MacOSX-SDKs/releases/download/11.3/MacOSX11.3.sdk.tar.xz" && \
    tar --strip-components=1 -C /opt/macosx-sdk -xf /tmp/macosx-sdk.tar.xz && \
    rm /tmp/macosx-sdk.tar.xz

# Install Windows and macOS build tools
RUN env GOBIN=/usr/local/bin go install github.com/konoui/lipo@v0.9.3 && \
    env GOBIN=/usr/local/bin go install github.com/tc-hib/go-winres@latest

# Verify installations
RUN go version && zig version

# Build libusb for each target architecture
FROM base AS libusb-builder

# Copy build script
COPY build_c_lib.sh /usr/local/bin/build_c_lib.sh
RUN chmod +x /usr/local/bin/build_c_lib.sh

# Define target architectures and their corresponding host triplets
ENV TARGETS="x86_64-linux-gnu aarch64-linux-gnu arm-linux-gnueabihf"
ENV HOSTS="x86_64-linux aarch64-linux arm-linux"

WORKDIR /src
RUN git clone --depth 1 https://github.com/libusb/libusb.git
WORKDIR /src/libusb

# Generate configure script
RUN ./autogen.sh

# Build libusb for all targets using the consolidated script
RUN /usr/local/bin/build_c_lib.sh libusb "/opt/deps" "--disable-udev --enable-static-programs" "$TARGETS" "$HOSTS"

# Build libusb-compat-0.1 for each target (provides old libusb-0.1 API on top of libusb-1.0)
WORKDIR /src
RUN git clone --depth 1 https://github.com/libusb/libusb-compat-0.1.git
WORKDIR /src/libusb-compat-0.1

# Generate configure script for libusb-compat
RUN ./bootstrap.sh

# Build libusb-compat for all targets using the consolidated script
RUN /usr/local/bin/build_c_lib.sh libusb-compat "/opt/deps" "" "$TARGETS" "$HOSTS"

# Build libnfc for each target architecture
FROM libusb-builder AS libnfc-builder

WORKDIR /src
RUN git clone --depth 1 https://github.com/nfc-tools/libnfc.git
WORKDIR /src/libnfc

# Generate configure script for libnfc
RUN autoreconf -vis

# Build libnfc for all targets using the consolidated script
RUN /usr/local/bin/build_c_lib.sh libnfc "/opt/libnfc" "--with-drivers=acr122_usb,pn532_i2c,pn532_uart" "$TARGETS" "$HOSTS"

# Build minimal alsa-lib for each target architecture (for audio support via beep/oto)
FROM libnfc-builder AS alsa-builder

# Copy alsa build script
COPY build_alsa_lib.sh /usr/local/bin/build_alsa_lib.sh
RUN chmod +x /usr/local/bin/build_alsa_lib.sh

WORKDIR /src
ENV ALSA_LIB_VERSION=1.2.14
RUN wget ftp://ftp.alsa-project.org/pub/lib/alsa-lib-${ALSA_LIB_VERSION}.tar.bz2 && \
    tar -xf alsa-lib-${ALSA_LIB_VERSION}.tar.bz2
WORKDIR /src/alsa-lib-${ALSA_LIB_VERSION}

# Build alsa-lib for all targets using the build script
RUN /usr/local/bin/build_alsa_lib.sh "$TARGETS" "$HOSTS"

# Final builder stage with all cross-compiled libraries
FROM base AS builder

# Copy all pre-built dependencies
COPY --from=alsa-builder /opt /opt

# Install task runner
RUN sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

# Create non-root build user for security
ARG UID=1000
ARG GID=1000
RUN addgroup -g $GID build && \
    adduser -D -u $UID -G build build

# Switch to non-root user
USER build
WORKDIR /build

# Configure git for the build user
RUN git config --global --add safe.directory /build

# Set up Zig environment to avoid writing to /usr
ENV ZIG_GLOBAL_CACHE_DIR="/home/build/zig-cache"

# Set up Go cache directories for fast iterative builds
RUN mkdir -p /home/build/.cache/go-build /home/build/go
