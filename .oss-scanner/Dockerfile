# Build image for Anthropic's OSS Scanner (https://github.com/anthropics/oss-scanner).
#
# The scanner builds this file with the repository checkout as the build
# context, then audits the result with no network access. Everything a build,
# test or fuzz run needs must therefore be fetched here.
#
# The Debian release follows scripts/zigcc/Dockerfile, which builds the release
# binaries.
FROM golang:1.27-trixie

# The only native libraries a Linux build needs: libnfc for the clausecker/nfc
# binding and libpcsclite for the scard binding. Same list as the Ubuntu job in
# .github/workflows/lint-and-test.yml.
RUN apt-get update && apt-get install -y --no-install-recommends \
      libnfc-dev libpcsclite-dev \
    && rm -rf /var/lib/apt/lists/*

# Pinned to the version CI installs.
RUN go install github.com/go-task/task/v3/cmd/task@v3.44.1

# The Linux service refuses to start as root, and the audit shell is root.
RUN useradd --create-home zaparoo

# Scanner contract: the checkout lives inside the image at /src.
COPY . /src
WORKDIR /src

# Use exactly the Go release go.mod names, whatever patch release the base image
# carries: scripts/expr-method-profile pins its reviewed profile to it. The base
# image sets GOTOOLCHAIN=local, which would override the setting written here.
ENV GOTOOLCHAIN=
RUN go env -w GOTOOLCHAIN="go$(sed -n 's/^go //p' go.mod)" && go version

# mister/ is a nested module that ./... does not reach.
RUN go mod download && (cd mister && go mod download)

# The Linux service binary with the release build tags from Taskfile.dist.yml,
# but dynamically linked and unstripped so a debugger has symbols.
RUN go run ./scripts/compress-app \
    && go build \
      -ldflags "-X 'github.com/ZaparooProject/zaparoo-core/v2/pkg/config.AppVersion=oss-scanner'" \
      -tags "netgo,osusergo,sqlite_omit_load_extension,validator_novalidatefn,expr_static_methods" \
      -o /src/_build/linux_amd64/zaparoo \
      ./cmd/linux

# Run the suite as CI does. This proves the image works and leaves a warm build
# cache, so tests and fuzz targets start quickly offline. A failing test is
# reported but does not fail the image.
RUN (timeout 25m go test -race -tags=validator_novalidatefn,expr_static_methods ./... \
      || echo "WARNING: go test reported failures") \
    && (cd mister && (timeout 5m go test -race ./... \
      || echo "WARNING: mister go test reported failures"))

# The audit has no network: fail at once instead of waiting on the module proxy.
ENV GOPROXY=off
