# Zaparoo HyperHQ Plugin Build Container
# Copyright (c) 2026 The Zaparoo Project Contributors.
# SPDX-License-Identifier: GPL-3.0-or-later

FROM zaparoo/zigcc AS build

USER build
WORKDIR /src

# Copy module files and download deps first to maximise layer caching.
COPY --chown=build:build go.mod go.sum ./
RUN go mod download

# Copy source and build a static Windows AMD64 binary. CGO is disabled because
# none of the plugin's dependencies require it; this keeps the artefact small
# and self-contained.
COPY --chown=build:build . .
RUN env GOOS=windows GOARCH=amd64 CGO_ENABLED=0 \
    go build -trimpath -ldflags='-s -w -H windowsgui' \
    -o /build/zaparoo-hyperhq.exe .
