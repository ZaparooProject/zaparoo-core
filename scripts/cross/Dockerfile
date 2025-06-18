FROM ubuntu:plucky

RUN apt-get update -y && apt-get upgrade -y
RUN apt-get install ca-certificates openssl -y && update-ca-certificates
RUN apt-get install build-essential git curl golang-go gcc-mingw-w64 -y

# install zig
RUN mkdir -p /opt/zig && \
    curl -sSL -o /tmp/zig.tar.xz \
    "https://ziglang.org/builds/zig-linux-$(uname -m)-0.15.0-dev.552+bc2f7c754.tar.xz" && \
    tar --strip-components=1 -C /opt/zig -xf /tmp/zig.tar.xz && \
    rm /tmp/zig.tar.xz

# install macos sdk
RUN mkdir -p /opt/macosx-sdk && \
    curl -sSL -o /tmp/macosx-sdk.tar.xz \
    "https://github.com/phracker/MacOSX-SDKs/releases/download/11.3/MacOSX11.3.sdk.tar.xz" && \
    tar --strip-components=1 -C /opt/macosx-sdk -xf /tmp/macosx-sdk.tar.xz && \
    rm /tmp/macosx-sdk.tar.xz

# install build tools
RUN sh -c "$(curl --location https://taskfile.dev/install.sh)" -- -d -b /usr/local/bin
RUN env GOBIN=/usr/local/bin go install github.com/konoui/lipo@v0.9.3
RUN env GOBIN=/usr/local/bin go install github.com/tc-hib/go-winres@latest

# match output file permissions with host
ARG UID=1000
ARG GID=1000
RUN groupadd -f -g $GID build && \
    useradd -m -u $UID -g $GID build
USER build

ENV PATH="/opt/zig:${PATH}"
# avoids an error where zig tries to write to /usr
ENV ZIG_GLOBAL_CACHE_DIR="/home/build/zig-cache"

RUN git config --global --add safe.directory /build
WORKDIR /build
