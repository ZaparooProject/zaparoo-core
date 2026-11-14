# Zaparoo Playnite Extension Build Container
# Copyright (c) 2026 The Zaparoo Project Contributors.
# SPDX-License-Identifier: GPL-3.0-or-later

FROM mcr.microsoft.com/dotnet/sdk:9.0 AS build

WORKDIR /src

COPY ZaparooPlaynite.csproj .
RUN dotnet restore

COPY . .
RUN dotnet build -c Release --no-restore -o /build
