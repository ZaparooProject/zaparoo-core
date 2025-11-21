# Zaparoo LaunchBox Plugin Build Container
# Copyright (c) 2025 The Zaparoo Project Contributors.
# SPDX-License-Identifier: GPL-3.0-or-later

FROM mcr.microsoft.com/dotnet/sdk:9.0 AS build

WORKDIR /src

# Copy project file and LaunchBox Plugin API DLL
COPY ZaparooLaunchBoxPlugin.csproj .
COPY lib/Unbroken.LaunchBox.Plugins.dll lib/

# Restore dependencies
RUN dotnet restore

# Copy source files
COPY . .

# Publish the plugin (creates complete artifact)
RUN dotnet publish -c Release -o /build --no-restore
