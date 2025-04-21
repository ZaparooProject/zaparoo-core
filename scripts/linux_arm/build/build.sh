#!/bin/bash

export GO111MODULE=on
export GOPROXY=https://goproxy.io,direct
export CGO_ENABLED=1
export CGO_LDFLAGS="-lpcsclite -lnfc -lusb"

go build \
    -ldflags "-X 'github.com/ZaparooProject/zaparoo-core/pkg/config.AppVersion=${3}' -linkmode external -extldflags -static -s -w" \
    -tags netgo -o "_build/${1}_${2}/zaparoo" "./cmd/${1}"
