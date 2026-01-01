FROM debian:trixie AS base

# Install base dependencies
RUN set -eux; \
    apt-get update; \
    apt-get install -y --no-install-recommends \
        ca-certificates \
        curl \
        git \
        build-essential \
        autoconf \
        automake \
        libtool \
        pkg-config \
        linux-libc-dev \
        bash \
        libudev-dev \
        mingw-w64 \
        xz-utils; \
    rm -rf /var/lib/apt/lists/*

# Install Go
COPY --from=golang:1.24 /usr/local/go/ /usr/local/go/
ENV PATH="/usr/local/go/bin:${PATH}"

# Install Zig
ENV ZIG_VERSION=0.14.0
RUN ARCH=$(uname -m | sed 's/arm64/aarch64/g') && \
    curl -L "https://ziglang.org/download/${ZIG_VERSION}/zig-linux-${ARCH}-${ZIG_VERSION}.tar.xz" | \
    tar -C /usr/local/bin --strip-components=1 -xJ

# Install macOS SDK (12.3 required for SecTrustCopyCertificateChain)
RUN mkdir -p /opt/macosx-sdk && \
    curl -sSL -o /tmp/macosx-sdk.tar.xz \
    "https://github.com/joseluisq/macosx-sdks/releases/download/12.3/MacOSX12.3.sdk.tar.xz" && \
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

# Final builder stage with all cross-compiled libraries
FROM base AS builder

# Copy all pre-built dependencies (libnfc, libusb, libusb-compat)
COPY --from=libnfc-builder /opt /opt

# Install task runner
RUN sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin

# Create non-root build user for security
ARG UID=1000
ARG GID=1000
# Create non-root build user with adjusted GID
RUN set -eux; \
    groupadd -g "$GID" build || groupadd -g "$((GID + 1000))" build; \
    useradd -m -u "$UID" -g build -s /bin/bash build

# Switch to non-root user
USER build
WORKDIR /build

# Configure git for the build user
RUN git config --global --add safe.directory /build

# Set up Zig environment to avoid writing to /usr
ENV ZIG_GLOBAL_CACHE_DIR="/home/build/zig-cache"

# Set up Go cache directories for fast iterative builds
RUN mkdir -p /home/build/.cache/go-build /home/build/go
