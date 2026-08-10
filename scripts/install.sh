#!/usr/bin/env bash
# Zaparoo Core Universal Installer
# Copyright (c) 2026 The Zaparoo Project Contributors.
# SPDX-License-Identifier: GPL-3.0-or-later

set -e          # Exit on error
set -o pipefail # Fail on pipeline errors
set -u          # Treat unset variables as errors

# ============================================================================
# Configuration
# ============================================================================

GITHUB_REPO="ZaparooProject/zaparoo-core"
BASE_URL="https://github.com/${GITHUB_REPO}/releases"
GITHUB_API_URL="https://api.github.com/repos/${GITHUB_REPO}"
UPDATE_BASE_URL="https://updates.zaparoo.org/${GITHUB_REPO}"
UPDATE_PUBLIC_KEY='-----BEGIN PUBLIC KEY-----
MCowBQYDK2VwAyEA/Lk2OtGRI9eAaQ+F+Nuwk4k0bSmWfhVsNUGm159QkJg=
-----END PUBLIC KEY-----'

INSTALLER_VERSION="2"
MODE="install"
CHANNEL="stable"
VERSION=""
VERSION_TAG=""
DRY_RUN=false
NONINTERACTIVE="${NONINTERACTIVE:-}"
TMP_DIR=""
APP_PATH="${HOME}/.local/bin/zaparoo"
DECKY_REPO="ZaparooProject/zaparoo-decky"
DECKY_API_URL="https://api.github.com/repos/${DECKY_REPO}"
DECKY_HOME="${HOME}/homebrew"
DECKY_PLUGIN_PATH="${DECKY_HOME}/plugins/Zaparoo"
DECKY_SERVICE="plugin_loader.service"
DECKY_MAX_ARCHIVE_BYTES=20971520
DECKY_MINIMUM_CORE_VERSION="2.17.0"

# ============================================================================
# Color and Output Functions
# ============================================================================

# Check if we're writing to a terminal
if [ -t 1 ]; then
    BOLD="$(tput bold 2>/dev/null || echo '')"
    BLUE="$(tput setaf 4 2>/dev/null || echo '')"
    GREEN="$(tput setaf 2 2>/dev/null || echo '')"
    RED="$(tput setaf 1 2>/dev/null || echo '')"
    YELLOW="$(tput setaf 3 2>/dev/null || echo '')"
    RESET="$(tput sgr0 2>/dev/null || echo '')"
else
    BOLD=""
    BLUE=""
    GREEN=""
    RED=""
    YELLOW=""
    RESET=""
fi

info() {
    printf "${BLUE}${BOLD}==>${RESET} ${BOLD}%s${RESET}\n" "$*"
}

success() {
    printf "${GREEN}${BOLD}✓${RESET} %s\n" "$*"
}

error() {
    printf "${RED}${BOLD}✗ Error:${RESET} %s\n" "$*" >&2
}

warn() {
    printf "${YELLOW}${BOLD}▸ Warning:${RESET} %s\n" "$*" >&2
}

abort() {
    error "$@"
    exit 1
}

# ============================================================================
# System Detection
# ============================================================================

detect_os() {
    local os
    os="$(uname -s)"

    case "${os}" in
        Linux)
            echo "linux"
            ;;
        Darwin)
            echo "macos"
            ;;
        CYGWIN*|MINGW*|MSYS*)
            echo "windows"
            ;;
        *)
            echo "unknown"
            ;;
    esac
}

detect_linux_distro() {
    # Check for MiSTer FPGA (before os-release check)
    if [ -f /MiSTer.version ]; then
        echo "mister"
        return
    fi

    # Check for RePlayOS (before os-release check — it reports as plain Debian)
    if [ -d /opt/replay ]; then
        echo "replayos"
        return
    fi

    # Detect Linux distribution from /etc/os-release
    if [ ! -f /etc/os-release ]; then
        echo "generic"
        return
    fi

    # Source the os-release file to get distro info
    # shellcheck source=/dev/null
    . /etc/os-release

    # Check NAME field for distributions that use generic IDs
    # Batocera uses ID=buildroot, so we need to check NAME
    if echo "${NAME:-}" | grep -qi "batocera"; then
        echo "batocera"
        return
    fi

    # Return the ID (e.g., "ubuntu", "fedora", "steamos", "chimeraos")
    echo "${ID:-generic}"
}

detect_arch() {
    local arch
    arch="$(uname -m)"

    case "${arch}" in
        x86_64)
            echo "amd64"
            ;;
        aarch64|arm64)
            echo "arm64"
            ;;
        armv7l)
            echo "arm"
            ;;
        *)
            abort "Unsupported architecture: ${arch}"
            ;;
    esac
}

# ============================================================================
# Prerequisite Checks
# ============================================================================

require_command() {
    local command_name="$1"
    if ! command -v "${command_name}" >/dev/null 2>&1; then
        abort "${command_name} is required but not installed. Please install it and try again."
    fi
}

check_requirements() {
    info "Checking requirements..."

    if [ -z "${BASH_VERSION:-}" ]; then
        abort "Bash is required to run this installer"
    fi

    require_command curl
    require_command tar

    if [ "${MODE}" = "install" ]; then
        require_command openssl
        require_command sha256sum
    fi

    success "All requirements met"
}

# ============================================================================
# Download and Extract
# ============================================================================

ensure_tmp_dir() {
    if [ -z "${TMP_DIR:-}" ]; then
        TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'zaparoo-install')"
    fi
}

signed_checksum_for() {
    local filename="$1"
    awk -v filename="${filename}" '$2 == filename { print $1; exit }' "${SIGNED_CHECKSUMS}"
}

verify_file_checksum() {
    local path="$1"
    local filename="$2"
    local expected actual

    expected="$(signed_checksum_for "${filename}")"
    if ! echo "${expected}" | grep -Eq '^[0-9a-fA-F]{64}$'; then
        error "Signed checksum is missing for ${filename}"
        return 1
    fi
    actual="$(sha256sum "${path}" | awk '{ print $1 }')"
    if [ "${actual}" != "${expected}" ]; then
        error "Checksum verification failed for ${filename}"
        return 1
    fi
    return 0
}

resolve_version() {
    local requested selected release_api release_metadata

    ensure_tmp_dir
    SIGNED_CHECKSUMS="${TMP_DIR}/checksums.txt"
    local signature_path="${TMP_DIR}/checksums.txt.sig"
    local public_key_path="${TMP_DIR}/update-signing.pem"

    info "Loading signed release checksums..."
    curl --fail --silent --show-error --location "${UPDATE_BASE_URL}/checksums.txt" -o "${SIGNED_CHECKSUMS}" || \
        abort "Failed to download signed checksums"
    curl --fail --silent --show-error --location "${UPDATE_BASE_URL}/checksums.txt.sig" -o "${signature_path}" || \
        abort "Failed to download checksum signature"
    printf '%s\n' "${UPDATE_PUBLIC_KEY}" > "${public_key_path}"

    if ! openssl pkeyutl -verify -pubin -inkey "${public_key_path}" -rawin \
        -in "${SIGNED_CHECKSUMS}" -sigfile "${signature_path}" >/dev/null 2>&1; then
        abort "Release checksum signature verification failed"
    fi

    requested="${ZAPAROO_VERSION:-}"
    if [ -n "${requested}" ]; then
        selected="${requested}"
    else
        release_metadata="${TMP_DIR}/release.json"
        if [ "${CHANNEL}" = "beta" ]; then
            release_api="${GITHUB_API_URL}/releases?per_page=1"
        else
            release_api="${GITHUB_API_URL}/releases/latest"
        fi
        curl --fail --silent --show-error --location "${release_api}" -o "${release_metadata}" || \
            abort "Failed to resolve the latest ${CHANNEL} release"
        selected="$(sed -n 's/^[[:space:]]*"tag_name":[[:space:]]*"\([^"]*\)".*/\1/p' \
            "${release_metadata}" | head -n 1)"
    fi
    selected="${selected#v}"
    if ! echo "${selected}" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([+.-][0-9A-Za-z.-]+)?$'; then
        abort "Invalid ${CHANNEL} release version: ${selected:-missing}"
    fi

    VERSION="${selected}"
    VERSION_TAG="v${VERSION}"
    success "Selected Zaparoo Core ${VERSION} (${CHANNEL})"
}

download_and_extract() {
    local os_type arch archive_name download_url

    os_type="$1"
    arch="$(detect_arch)"

    info "Detected system: ${os_type}/${arch}"

    archive_name="zaparoo-${os_type}_${arch}-${VERSION}.tar.gz"
    download_url="${BASE_URL}/download/${VERSION_TAG}/${archive_name}"

    info "Downloading Zaparoo Core ${VERSION}..."
    info "URL: ${download_url}"

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would download and verify: ${archive_name}"
        info "[DRY-RUN] Would extract to temporary directory"
        ZAPAROO_BIN="/tmp/zaparoo-dry-run"
        return 0
    fi

    ensure_tmp_dir
    TMP_ARCHIVE="${TMP_DIR}/${archive_name}"
    TMP_EXTRACT="${TMP_DIR}/extract-${os_type}"

    if ! curl --fail --progress-bar --location "${download_url}" -o "${TMP_ARCHIVE}"; then
        abort "Failed to download from ${download_url}"
    fi
    verify_file_checksum "${TMP_ARCHIVE}" "${archive_name}" || abort "Downloaded archive verification failed"
    success "Downloaded and verified ${archive_name}"

    info "Extracting archive..."
    mkdir -p "${TMP_EXTRACT}"

    if ! tar -xzf "${TMP_ARCHIVE}" -C "${TMP_EXTRACT}"; then
        abort "Failed to extract archive"
    fi

    ZAPAROO_BIN="$(find "${TMP_EXTRACT}" -type f -name "zaparoo" | head -n 1)"
    if [ -z "${ZAPAROO_BIN}" ] || [ ! -f "${ZAPAROO_BIN}" ]; then
        abort "Could not find zaparoo binary in archive"
    fi

    chmod +x "${ZAPAROO_BIN}"
    success "Extracted to temporary location"
}

# ============================================================================
# SteamOS Transactional Installation
# ============================================================================

installed_version() {
    if [ ! -x "${APP_PATH}" ]; then
        return 0
    fi
    "${APP_PATH}" -version 2>/dev/null | sed -n 's/^Zaparoo v\([^ ]*\).*/\1/p' | head -n 1
}

is_semver() {
    echo "$1" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+([+.-][0-9A-Za-z.-]+)?$'
}

semver_compare() {
    if ! is_semver "$1" || ! is_semver "$2"; then
        return 2
    fi

    local left="${1%%+*}"
    local right="${2%%+*}"
    local left_core="${left%%-*}"
    local right_core="${right%%-*}"
    local left_pre=""
    local right_pre=""
    local index left_id right_id
    local -a left_parts right_parts left_pre_parts right_pre_parts

    if [[ "${left}" == *-* ]]; then
        left_pre="${left#*-}"
    fi
    if [[ "${right}" == *-* ]]; then
        right_pre="${right#*-}"
    fi
    IFS='.' read -r -a left_parts <<< "${left_core}"
    IFS='.' read -r -a right_parts <<< "${right_core}"
    for index in 0 1 2; do
        if ((10#${left_parts[index]} < 10#${right_parts[index]})); then
            echo -1
            return
        fi
        if ((10#${left_parts[index]} > 10#${right_parts[index]})); then
            echo 1
            return
        fi
    done
    if [ -z "${left_pre}" ] && [ -z "${right_pre}" ]; then
        echo 0
        return
    fi
    if [ -z "${left_pre}" ]; then
        echo 1
        return
    fi
    if [ -z "${right_pre}" ]; then
        echo -1
        return
    fi

    IFS='.' read -r -a left_pre_parts <<< "${left_pre}"
    IFS='.' read -r -a right_pre_parts <<< "${right_pre}"
    for ((index = 0; index < ${#left_pre_parts[@]} || index < ${#right_pre_parts[@]}; index++)); do
        if ((index >= ${#left_pre_parts[@]})); then
            echo -1
            return
        fi
        if ((index >= ${#right_pre_parts[@]})); then
            echo 1
            return
        fi
        left_id="${left_pre_parts[index]}"
        right_id="${right_pre_parts[index]}"
        if [ "${left_id}" = "${right_id}" ]; then
            continue
        fi
        if [[ "${left_id}" =~ ^[0-9]+$ ]] && [[ "${right_id}" =~ ^[0-9]+$ ]]; then
            if ((10#${left_id} < 10#${right_id})); then
                echo -1
            else
                echo 1
            fi
            return
        fi
        if [[ "${left_id}" =~ ^[0-9]+$ ]]; then
            echo -1
            return
        fi
        if [[ "${right_id}" =~ ^[0-9]+$ ]]; then
            echo 1
            return
        fi
        if [[ "${left_id}" < "${right_id}" ]]; then
            echo -1
        else
            echo 1
        fi
        return
    done
    echo 0
}

upgrade_relation() {
    local installed="$1"
    local selected="$2"
    if ! is_semver "${installed}"; then
        echo -1
        return
    fi
    semver_compare "${installed}" "${selected}"
}

core_api_available() {
    curl --fail --silent --show-error "http://127.0.0.1:7497/health" 2>/dev/null | \
        grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"'
}

wait_for_core_api_down() {
    local attempt
    for ((attempt = 0; attempt < 20; attempt++)); do
        if ! core_api_available; then
            return 0
        fi
        sleep 0.25
    done
    return 1
}

json_string_field() {
    local field="$1"
    local json="$2"
    printf '%s\n' "${json}" | sed -n \
        's/.*"'"${field}"'"[[:space:]]*:[[:space:]]*"\([^"]*\)".*/\1/p' | head -n 1
}

verify_core_api() {
    local expected_version="$1"
    local attempt health version_response actual_version actual_platform
    for ((attempt = 0; attempt < 30; attempt++)); do
        health="$(curl --fail --silent --show-error "http://127.0.0.1:7497/health" 2>/dev/null || true)"
        if echo "${health}" | grep -Eq '"status"[[:space:]]*:[[:space:]]*"ok"'; then
            version_response="$(curl --fail --silent --show-error \
                -H 'Content-Type: application/json' \
                --data '{"jsonrpc":"2.0","id":"installer","method":"version"}' \
                "http://127.0.0.1:7497/api" 2>/dev/null || true)"
            actual_version="$(json_string_field "version" "${version_response}")"
            actual_platform="$(json_string_field "platform" "${version_response}")"
            if [ "${actual_version}" = "${expected_version}" ] && [ "${actual_platform}" = "steamos" ]; then
                return 0
            fi
        fi
        sleep 0.5
    done
    return 1
}

rollback_steamos_transaction() {
    local previous_version="$1"
    local backup_path="$2"

    warn "Rolling back failed Zaparoo Core transaction"
    systemctl --user stop zaparoo.service >/dev/null 2>&1 || true
    if [ -n "${previous_version}" ] && [ -f "${backup_path}" ]; then
        rm -f "${APP_PATH}" "${APP_PATH}.new"
        mv -f "${backup_path}" "${APP_PATH}" || return 1
        "${APP_PATH}" -install application >/dev/null 2>&1 || true
        "${APP_PATH}" -install service >/dev/null 2>&1 || true
        "${APP_PATH}" -install steam-runtime >/dev/null 2>&1 || true
        systemctl --user daemon-reload >/dev/null 2>&1 || true
        systemctl --user enable --now zaparoo.service >/dev/null 2>&1 || true
    else
        if [ -x "${APP_PATH}" ]; then
            "${APP_PATH}" -uninstall service >/dev/null 2>&1 || true
            "${APP_PATH}" -uninstall steam-runtime >/dev/null 2>&1 || true
            "${APP_PATH}" -uninstall desktop >/dev/null 2>&1 || true
            "${APP_PATH}" -uninstall application >/dev/null 2>&1 || true
        fi
        rm -f "${APP_PATH}" "${APP_PATH}.new"
        systemctl --user disable zaparoo.service >/dev/null 2>&1 || true
        systemctl --user daemon-reload >/dev/null 2>&1 || true
    fi
}

install_steamos_transaction() {
    local current relation response backup_path had_desktop=false service_was_active=false

    require_command systemctl
    current="$(installed_version)"
    if [ -L "${APP_PATH}" ]; then
        abort "Existing ${APP_PATH} is a symbolic link; refusing to replace it"
    fi
    if [ -e "${APP_PATH}" ] && [ -z "${current}" ]; then
        abort "Existing ${APP_PATH} is not a recognized Zaparoo Core binary; refusing to replace it"
    fi
    if [ -n "${current}" ]; then
        if ! is_semver "${current}"; then
            warn "Installed development build ${current} will be replaced by release ${VERSION}"
        fi
        relation="$(upgrade_relation "${current}" "${VERSION}")"
        if [ "${relation}" -gt 0 ]; then
            abort "Installed Zaparoo Core ${current} is newer than selected ${VERSION}; refusing to downgrade"
        fi
        if [ "${relation}" -eq 0 ]; then
            if systemctl --user is-active --quiet zaparoo.service 2>/dev/null; then
                if ! verify_core_api "${current}"; then
                    abort "Zaparoo Core ${current} service is active but failed API verification"
                fi
            elif core_api_available; then
                abort "Zaparoo Core is running outside zaparoo.service; stop it before rerunning the installer"
            elif ! systemctl --user enable --now zaparoo.service || ! verify_core_api "${current}"; then
                abort "Zaparoo Core ${current} is installed but could not be started and verified"
            fi
            success "Zaparoo Core ${current} is already current and healthy"
            install_hardware "${APP_PATH}"
            offer_decky_plugin
            return 0
        fi
    fi

    download_and_extract "steamos"

    if [ "$DRY_RUN" = true ]; then
        if [ -n "${current}" ]; then
            info "[DRY-RUN] Would transactionally update Zaparoo Core ${current} to ${VERSION}"
        else
            info "[DRY-RUN] Would atomically install ${VERSION} to ${APP_PATH}"
        fi
        info "[DRY-RUN] Would enable and start zaparoo.service"
        info "[DRY-RUN] Would verify API health, platform, and version"
        offer_decky_plugin
        return 0
    fi

    if ! "${ZAPAROO_BIN}" -version | grep -Fq "Zaparoo v${VERSION} (steamos)"; then
        abort "Downloaded binary did not report expected SteamOS version ${VERSION}"
    fi

    mkdir -p "$(dirname "${APP_PATH}")"
    ensure_tmp_dir
    backup_path="${TMP_DIR}/zaparoo.rollback"
    if [ -f "${HOME}/.local/share/applications/zaparoo.desktop" ]; then
        had_desktop=true
    fi
    if systemctl --user is-active --quiet zaparoo.service 2>/dev/null; then
        service_was_active=true
        systemctl --user stop zaparoo.service || abort "Failed to stop zaparoo.service"
    elif core_api_available; then
        abort "Zaparoo Core is running outside zaparoo.service; stop it before rerunning the installer"
    fi
    if ! wait_for_core_api_down; then
        if [ "${service_was_active}" = true ]; then
            systemctl --user start zaparoo.service >/dev/null 2>&1 || true
        fi
        abort "Another Zaparoo Core instance is still using 127.0.0.1:7497"
    fi
    if [ -n "${current}" ] && ! cp "${APP_PATH}" "${backup_path}"; then
        if [ "${service_was_active}" = true ]; then
            systemctl --user start zaparoo.service >/dev/null 2>&1 || true
        fi
        abort "Failed to preserve the installed Core binary"
    fi
    if ! cp "${ZAPAROO_BIN}" "${APP_PATH}.new" || ! chmod 0755 "${APP_PATH}.new" || \
        ! mv -f "${APP_PATH}.new" "${APP_PATH}"; then
        rollback_steamos_transaction "${current}" "${backup_path}" || true
        abort "Failed to install application binary"
    fi

    if ! "${APP_PATH}" -install application || ! "${APP_PATH}" -install service || \
        ! "${APP_PATH}" -install steam-runtime; then
        rollback_steamos_transaction "${current}" "${backup_path}" || true
        abort "Failed to install Core integration files"
    fi

    if [ -z "${current}" ]; then
        response="$(prompt_yes_no "Install desktop shortcut?" "y")"
        if [ "${response}" = "y" ] && ! "${APP_PATH}" -install desktop; then
            rollback_steamos_transaction "${current}" "${backup_path}" || true
            abort "Failed to install desktop shortcut"
        fi
    elif [ "${had_desktop}" = true ] && ! "${APP_PATH}" -install desktop; then
        rollback_steamos_transaction "${current}" "${backup_path}" || true
        abort "Failed to refresh desktop shortcut"
    fi

    if ! systemctl --user daemon-reload || ! systemctl --user enable --now zaparoo.service; then
        rollback_steamos_transaction "${current}" "${backup_path}" || true
        abort "Failed to enable and start zaparoo.service"
    fi
    if ! verify_core_api "${VERSION}"; then
        rollback_steamos_transaction "${current}" "${backup_path}" || true
        abort "Zaparoo Core failed API health or version verification"
    fi

    rm -f "${backup_path}"
    success "Zaparoo Core ${VERSION} is healthy and running"
    install_hardware
    offer_decky_plugin
}

# ============================================================================
# Optional Decky Plugin Installation
# ============================================================================

decky_is_installed() {
    [ "$(uname -m)" = "x86_64" ] && \
        [ -d "${DECKY_HOME}/plugins" ] && \
        [ "$(systemctl show --property=LoadState --value "${DECKY_SERVICE}" 2>/dev/null || true)" = "loaded" ]
}

installed_decky_version() {
    local package_path="${DECKY_PLUGIN_PATH}/package.json"
    if [ ! -f "${package_path}" ]; then
        return 0
    fi
    python3 - "${package_path}" <<'PY'
import json
import re
import sys

try:
    package = json.load(open(sys.argv[1], encoding="utf-8"))
    version = package.get("version", "")
    if package.get("name") == "zaparoo-decky" and re.fullmatch(
        r"\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?", version
    ):
        print(version)
except (OSError, ValueError, TypeError):
    pass
PY
}

resolve_decky_release() {
    local metadata_path="$1"
    python3 - "${metadata_path}" "${DECKY_REPO}" "${DECKY_MAX_ARCHIVE_BYTES}" <<'PY'
import json
import re
import sys

metadata_path, repository, maximum_size = sys.argv[1:]
with open(metadata_path, encoding="utf-8") as metadata_file:
    release = json.load(metadata_file)
tag = release.get("tag_name", "")
if release.get("draft") or release.get("prerelease") or not re.fullmatch(
    r"v\d+\.\d+\.\d+(?:[-+][0-9A-Za-z.-]+)?", tag
):
    raise SystemExit("latest Decky release metadata is invalid")
version = tag[1:]
asset_name = f"Zaparoo-v{version}.zip"
expected_url = f"https://github.com/{repository}/releases/download/{tag}/{asset_name}"
for asset in release.get("assets", []):
    if asset.get("name") != asset_name:
        continue
    digest = asset.get("digest", "")
    size = asset.get("size")
    url = asset.get("browser_download_url")
    if not re.fullmatch(r"sha256:[0-9a-fA-F]{64}", digest):
        raise SystemExit("Decky release asset has no valid GitHub SHA-256 digest")
    if not isinstance(size, int) or size <= 0 or size > int(maximum_size):
        raise SystemExit("Decky release asset size is invalid")
    if url != expected_url:
        raise SystemExit("Decky release asset URL is invalid")
    print(version, url, digest.removeprefix("sha256:"), size, sep="\t")
    break
else:
    raise SystemExit(f"Decky release is missing {asset_name}")
PY
}

extract_decky_archive() {
    local archive_path="$1"
    local output_path="$2"
    local expected_version="$3"
    python3 - "${archive_path}" "${output_path}" "${expected_version}" <<'PY'
import json
import shutil
import stat
import sys
import zipfile
from pathlib import Path, PurePosixPath

archive_path, output_path, expected_version = sys.argv[1:]
required = {
    "Zaparoo/LICENSE",
    "Zaparoo/README.md",
    "Zaparoo/dist/index.js",
    "Zaparoo/main.py",
    "Zaparoo/package.json",
    "Zaparoo/plugin.json",
}
maximum_members = 128
maximum_uncompressed = 50 * 1024 * 1024
with zipfile.ZipFile(archive_path) as archive:
    members = archive.infolist()
    if not members or len(members) > maximum_members:
        raise SystemExit("Decky archive member count is invalid")
    names = [member.filename for member in members]
    if len(names) != len(set(names)):
        raise SystemExit("Decky archive contains duplicate members")
    if sum(member.file_size for member in members) > maximum_uncompressed:
        raise SystemExit("Decky archive is too large after extraction")
    for member in members:
        name = member.filename
        path = PurePosixPath(name)
        mode = member.external_attr >> 16
        file_type = stat.S_IFMT(mode)
        if (
            "\\" in name
            or path.is_absolute()
            or not path.parts
            or path.parts[0] != "Zaparoo"
            or any(part in {"", ".", ".."} for part in path.parts)
            or file_type not in {0, stat.S_IFREG, stat.S_IFDIR}
        ):
            raise SystemExit(f"Decky archive member is unsafe: {name}")
    if not required.issubset(names):
        raise SystemExit("Decky archive is missing required files")
    package = json.loads(archive.read("Zaparoo/package.json"))
    plugin = json.loads(archive.read("Zaparoo/plugin.json"))
    if package.get("name") != "zaparoo-decky" or package.get("version") != expected_version:
        raise SystemExit("Decky package identity or version is invalid")
    if plugin.get("name") != "Zaparoo" or set(plugin.get("flags", [])) & {
        "_root",
        "debug",
        "remote-binary",
    }:
        raise SystemExit("Decky plugin metadata is invalid")
    root = Path(output_path)
    for member in members:
        destination = root.joinpath(*PurePosixPath(member.filename).parts)
        if member.is_dir():
            destination.mkdir(parents=True, exist_ok=True)
            destination.chmod(0o755)
            continue
        destination.parent.mkdir(parents=True, exist_ok=True)
        with archive.open(member) as source, destination.open("wb") as target:
            shutil.copyfileobj(source, target, length=1024 * 1024)
        destination.chmod(0o644)
PY
}

rollback_decky_plugin() {
    local backup_path="$1"
    sudo rm -rf -- "${DECKY_PLUGIN_PATH}"
    if [ -d "${backup_path}" ]; then
        sudo mv -- "${backup_path}" "${DECKY_PLUGIN_PATH}"
    fi
    sudo systemctl start "${DECKY_SERVICE}" >/dev/null 2>&1 || true
}

install_decky_stage() {
    local staged_plugin="$1"
    local backup_path="${DECKY_HOME}/plugins/.Zaparoo.backup.$$"
    local owner

    if [ -e "${backup_path}" ]; then
        abort "Decky rollback path already exists: ${backup_path}"
    fi
    owner="$(id -u):$(id -g)"
    info "Stopping Decky Loader for plugin replacement..."
    sudo systemctl stop "${DECKY_SERVICE}" || abort "Failed to stop Decky Loader"
    if [ -e "${DECKY_PLUGIN_PATH}" ] && ! sudo mv -- "${DECKY_PLUGIN_PATH}" "${backup_path}"; then
        sudo systemctl start "${DECKY_SERVICE}" >/dev/null 2>&1 || true
        abort "Failed to preserve installed Zaparoo Decky plugin"
    fi
    if ! sudo mv -- "${staged_plugin}" "${DECKY_PLUGIN_PATH}" || \
        ! sudo chown -R "${owner}" "${DECKY_PLUGIN_PATH}" || \
        ! sudo chown 0:0 "${DECKY_PLUGIN_PATH}"; then
        rollback_decky_plugin "${backup_path}"
        abort "Failed to install Zaparoo Decky plugin"
    fi
    if ! sudo systemctl start "${DECKY_SERVICE}" || \
        ! sudo systemctl is-active --quiet "${DECKY_SERVICE}"; then
        rollback_decky_plugin "${backup_path}"
        abort "Decky Loader failed to restart; previous plugin was restored"
    fi
    if [ -d "${backup_path}" ]; then
        sudo rm -rf -- "${backup_path}"
    fi
}

offer_decky_plugin() {
    local response metadata_path release_info decky_version asset_url expected_digest expected_size
    local current archive_path actual_digest actual_size stage_path relation core_version

    if ! decky_is_installed; then
        return 0
    fi
    core_version="$(installed_version)"
    if [ -z "${core_version}" ] || \
        [ "$(semver_compare "${core_version}" "${DECKY_MINIMUM_CORE_VERSION}")" -lt 0 ]; then
        warn "Zaparoo Decky requires Core ${DECKY_MINIMUM_CORE_VERSION} or newer; skipping plugin"
        return 0
    fi
    response="$(prompt_yes_no "Install or update the optional Zaparoo Decky plugin (restarts Decky Loader)?" "n")"
    if [ "${response}" != "y" ]; then
        info "Skipping optional Decky plugin"
        return 0
    fi
    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would download and verify latest stable Zaparoo Decky plugin"
        info "[DRY-RUN] Would replace only ${DECKY_PLUGIN_PATH} with rollback"
        info "[DRY-RUN] Would restart ${DECKY_SERVICE}"
        return 0
    fi

    require_command python3
    require_command sha256sum
    require_command sudo
    ensure_tmp_dir
    metadata_path="${TMP_DIR}/decky-release.json"
    info "Resolving latest stable Zaparoo Decky release..."
    curl --fail --silent --show-error --location --max-time 30 --max-filesize 1048576 \
        "${DECKY_API_URL}/releases/latest" -o "${metadata_path}" || \
        abort "Failed to resolve latest Zaparoo Decky release"
    release_info="$(resolve_decky_release "${metadata_path}")" || abort "Invalid Zaparoo Decky release metadata"
    IFS=$'\t' read -r decky_version asset_url expected_digest expected_size <<< "${release_info}"

    current="$(installed_decky_version)"
    if [ -n "${current}" ]; then
        relation="$(semver_compare "${current}" "${decky_version}")"
        if [ "${relation}" -gt 0 ]; then
            abort "Installed Zaparoo Decky ${current} is newer than stable ${decky_version}; refusing to downgrade"
        fi
        if [ "${relation}" -eq 0 ]; then
            success "Zaparoo Decky ${current} is already current"
            return 0
        fi
    fi

    archive_path="${TMP_DIR}/Zaparoo-v${decky_version}.zip"
    info "Downloading Zaparoo Decky ${decky_version}..."
    curl --fail --show-error --location --max-time 120 --max-filesize "${DECKY_MAX_ARCHIVE_BYTES}" \
        "${asset_url}" -o "${archive_path}" || abort "Failed to download Zaparoo Decky plugin"
    actual_size="$(wc -c < "${archive_path}" | tr -d '[:space:]')"
    if [ "${actual_size}" != "${expected_size}" ]; then
        abort "Zaparoo Decky archive size verification failed"
    fi
    actual_digest="$(sha256sum "${archive_path}" | awk '{ print $1 }')"
    if [ "${actual_digest}" != "${expected_digest}" ]; then
        abort "Zaparoo Decky archive checksum verification failed"
    fi

    stage_path="${TMP_DIR}/decky-stage"
    mkdir -p "${stage_path}"
    extract_decky_archive "${archive_path}" "${stage_path}" "${decky_version}" || \
        abort "Zaparoo Decky archive validation failed"
    install_decky_stage "${stage_path}/Zaparoo"
    success "Zaparoo Decky ${decky_version} installed"
}

repair_steamos() {
    local current response

    require_command systemctl
    current="$(installed_version)"
    if [ -z "${current}" ]; then
        abort "Zaparoo Core is not installed; use install mode first"
    fi
    VERSION="${current}"

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would reinstall application, service, Steam Runtime, and optional desktop integration"
        info "[DRY-RUN] Would restart zaparoo.service and verify API health"
        return 0
    fi

    if ! "${APP_PATH}" -install application; then
        abort "Failed to repair application metadata"
    fi
    if ! "${APP_PATH}" -install service; then
        abort "Failed to repair systemd user service"
    fi
    if ! "${APP_PATH}" -install steam-runtime; then
        abort "Failed to repair Steam Runtime shortcut"
    fi
    response="$(prompt_yes_no "Install desktop shortcut?" "y")"
    if [ "${response}" = "y" ] && ! "${APP_PATH}" -install desktop; then
        abort "Failed to repair desktop shortcut"
    fi
    if ! systemctl --user daemon-reload || ! systemctl --user enable zaparoo.service || \
        ! systemctl --user restart zaparoo.service; then
        abort "Failed to restart zaparoo.service"
    fi
    if ! verify_core_api "${VERSION}"; then
        abort "Zaparoo Core failed API health or version verification after repair"
    fi

    success "Zaparoo Core ${VERSION} integration repaired"
    install_hardware "${APP_PATH}"
}

status_steamos() {
    local current
    current="$(installed_version)"
    info "Zaparoo SteamOS status"
    printf "  Binary: %s\n" "${APP_PATH}"
    printf "  Installed version: %s\n" "${current:-not installed}"
    if systemctl --user is-enabled --quiet zaparoo.service 2>/dev/null; then
        printf "  Service enabled: yes\n"
    else
        printf "  Service enabled: no\n"
    fi
    if systemctl --user is-active --quiet zaparoo.service 2>/dev/null; then
        printf "  Service active: yes\n"
    else
        printf "  Service active: no\n"
    fi
    if [ -x "${APP_PATH}" ]; then
        printf "  Steam Runtime:\n"
        "${APP_PATH}" -steam-runtime-status || printf "    unavailable\n"
    fi
    if [ -n "${current}" ]; then
        VERSION="${current}"
        if verify_core_api "${current}"; then
            printf "  API health: ok\n"
        else
            printf "  API health: unavailable or version mismatch\n"
            return 1
        fi
    else
        printf "  API health: unavailable\n"
        return 1
    fi
}

uninstall_steamos() {
    local response
    if [ ! -x "${APP_PATH}" ]; then
        warn "Zaparoo application binary is not installed"
        systemctl --user disable --now zaparoo.service >/dev/null 2>&1 || true
        return 0
    fi

    "${APP_PATH}" -uninstall service || warn "Failed to remove service"
    "${APP_PATH}" -uninstall steam-runtime || warn "Failed to remove Steam Runtime files"
    "${APP_PATH}" -uninstall desktop || warn "Failed to remove desktop shortcut"
    response="$(prompt_yes_no "Remove hardware support (requires sudo)?" "n")"
    if [ "${response}" = "y" ]; then
        sudo "${APP_PATH}" -uninstall hardware || warn "Failed to remove hardware support"
    fi
    "${APP_PATH}" -uninstall application || abort "Failed to remove application"
    success "Zaparoo Core removed; config, databases, mappings, and media were preserved"
}

# ============================================================================
# Generic Linux Installation (Desktop)
# ============================================================================

install_linux_generic() {
    local distro
    distro="$(detect_linux_distro)"

    info "Detected Linux distribution: ${distro}"

    # Check for special distro handling
    case "${distro}" in
        mister)
            # MiSTer FPGA has its own installation method
            install_mister
            return 0
            ;;
        replayos)
            # RePlayOS has its own installation method
            install_replayos
            return 0
            ;;
        batocera)
            # Batocera has its own installation method
            install_batocera
            return 0
            ;;
        steamos)
            warn "Immutable filesystem detected (steamos)"
            warn "Installation will be user-local only"
            install_steamos_transaction
            return 0
            ;;
        chimeraos)
            warn "Immutable filesystem detected (chimeraos)"
            warn "Installation will be user-local only"
            ;;
        recalbox|retropie)
            warn "Embedded system detected (${distro})"
            warn "Some features may require manual configuration"
            ;;
    esac

    # Download and extract
    download_and_extract "linux"

    # Install application (always)
    install_application

    # Optional components
    install_service
    install_desktop
    install_hardware
}

install_application() {
    info "Installing application (binary, menu entry, icons)..."

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would install binary to: ~/.local/bin/zaparoo"
        info "[DRY-RUN] Would install menu entry to: ~/.local/share/applications/"
        info "[DRY-RUN] Would install icons to: ~/.local/share/icons/"
        success "[DRY-RUN] Application installation simulated"
        return 0
    fi

    if ! "${ZAPAROO_BIN}" -install application; then
        abort "Failed to install application files"
    fi

    ZAPAROO_BIN="${APP_PATH}"
    success "Application installed to ~/.local/bin/zaparoo"
}

prompt_yes_no() {
    local prompt="$1"
    local default="${2:-n}"

    # Dry-run mode - always answer yes to show what would be installed
    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Auto-answering prompt: yes" >&2
        echo "y"
        return
    fi

    # Non-interactive mode
    if [ -n "${NONINTERACTIVE:-}" ]; then
        echo "${default}"
        return
    fi

    # When run via "curl | bash", stdin is the pipe, not the terminal.
    # We need to read from /dev/tty for input. The prompt goes to stderr
    # so it's not captured by command substitution.
    local yn
    if [ "${default}" = "y" ]; then
        printf "%s [Y/n] " "${prompt}" >&2
    else
        printf "%s [y/N] " "${prompt}" >&2
    fi

    # Read from /dev/tty if available (handles curl|bash case), otherwise stdin
    if [ -e /dev/tty ]; then
        read -r yn </dev/tty
    else
        read -r yn
    fi
    yn="${yn:-${default}}"

    case "${yn}" in
        [Yy]*)
            echo "y"
            ;;
        *)
            echo "n"
            ;;
    esac
}

install_service() {
    local response
    response="$(prompt_yes_no "Install systemd service (auto-start on login)?" "y")"

    if [ "${response}" = "y" ]; then
        info "Installing systemd user service..."

        if [ "$DRY_RUN" = true ]; then
            info "[DRY-RUN] Would install service to: ~/.config/systemd/user/zaparoo.service"
            info "[DRY-RUN] Would enable with: systemctl --user enable zaparoo"
            info "[DRY-RUN] Would start with: systemctl --user start zaparoo"
            success "[DRY-RUN] Systemd service installation simulated"
            return 0
        fi

        if ! "${ZAPAROO_BIN}" -install service; then
            warn "Failed to install systemd service"
            return 1
        fi

        if ! systemctl --user enable --now zaparoo.service; then
            warn "Service installed but could not be enabled and started"
            return 1
        fi
        success "Systemd service installed, enabled, and started"
    else
        info "Skipping systemd service installation"
    fi
}

install_desktop() {
    local response
    response="$(prompt_yes_no "Install desktop shortcut?" "y")"

    if [ "${response}" = "y" ]; then
        info "Installing desktop shortcut..."

        if [ "$DRY_RUN" = true ]; then
            info "[DRY-RUN] Would install desktop file to: ~/.local/share/applications/zaparoo.desktop"
            success "[DRY-RUN] Desktop shortcut installation simulated"
            return 0
        fi

        if ! "${ZAPAROO_BIN}" -install desktop; then
            warn "Failed to install desktop shortcut"
            return 1
        fi

        success "Desktop shortcut installed"
    else
        info "Skipping desktop shortcut installation"
    fi
}

install_hardware() {
    local install_binary="${1:-${ZAPAROO_BIN}}"
    local response
    response="$(prompt_yes_no "Install hardware support (udev rules, requires sudo)?" "y")"

    if [ "${response}" = "y" ]; then
        info "Installing hardware support (requires root)..."

        if [ "$DRY_RUN" = true ]; then
            info "[DRY-RUN] Would install udev rules to: /etc/udev/rules.d/99-zaparoo.rules"
            info "[DRY-RUN] Would reload udev with: udevadm control --reload-rules"
            info "[DRY-RUN] Would require: sudo privileges"
            success "[DRY-RUN] Hardware support installation simulated"
            return 0
        fi

        if ! sudo "${install_binary}" -install hardware; then
            warn "Failed to install hardware support"
            return 1
        fi

        success "Hardware support installed"
        info "You may need to replug your reader or reboot for changes to take effect"
    else
        info "Skipping hardware support installation"
    fi
}

# ============================================================================
# MiSTer FPGA Installation
# ============================================================================

install_mister() {
    info "Installing Zaparoo Core for MiSTer FPGA..."

    local archive_name download_url
    archive_name="zaparoo-mister_arm-${VERSION}.zip"
    download_url="${BASE_URL}/download/${VERSION_TAG}/${archive_name}"

    info "Downloading Zaparoo Core ${VERSION}..."
    info "URL: ${download_url}"

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would download: ${archive_name}"
        info "[DRY-RUN] Would extract zaparoo.sh to: /media/fat/Scripts/zaparoo.sh"
        install_mister_startup_dryrun
        success "[DRY-RUN] MiSTer installation simulated"
        return 0
    fi

    # Use the metadata transaction directory so signed checksums remain available.
    ensure_tmp_dir
    TMP_ARCHIVE="${TMP_DIR}/${archive_name}"

    # Download archive
    if ! curl --fail --progress-bar --location "${download_url}" -o "${TMP_ARCHIVE}"; then
        abort "Failed to download from ${download_url}"
    fi

    verify_file_checksum "${TMP_ARCHIVE}" "${archive_name}" || abort "Downloaded archive verification failed"
    success "Downloaded and verified ${archive_name}"

    # Check for unzip
    if ! command -v unzip >/dev/null 2>&1; then
        abort "unzip is required for MiSTer installation but not found"
    fi

    # Extract zaparoo.sh
    info "Extracting to /media/fat/Scripts/..."
    if ! unzip -o "${TMP_ARCHIVE}" zaparoo.sh -d /media/fat/Scripts/; then
        abort "Failed to extract zaparoo.sh"
    fi

    chmod +x /media/fat/Scripts/zaparoo.sh
    success "Installed to /media/fat/Scripts/zaparoo.sh"

    # Prompt for startup
    install_mister_startup

    success "MiSTer installation complete!"
    info "Run from the Scripts menu or via SSH: /media/fat/Scripts/zaparoo.sh"
}

install_mister_startup() {
    local response
    response="$(prompt_yes_no "Add to MiSTer startup (auto-start on boot)?" "y")"

    if [ "${response}" = "y" ]; then
        info "Adding to MiSTer startup..."
        if ! /media/fat/Scripts/zaparoo.sh -add-startup; then
            warn "Failed to add to startup"
            return 1
        fi
        success "Added to MiSTer startup"
    else
        info "Skipping startup configuration"
        info "You can add it later with: /media/fat/Scripts/zaparoo.sh -add-startup"
    fi
}

install_mister_startup_dryrun() {
    local response
    response="$(prompt_yes_no "Add to MiSTer startup (auto-start on boot)?" "y")"

    if [ "${response}" = "y" ]; then
        info "[DRY-RUN] Would run: /media/fat/Scripts/zaparoo.sh -add-startup"
        success "[DRY-RUN] Would add to MiSTer startup"
    else
        info "[DRY-RUN] Skipping startup configuration"
    fi
}

# ============================================================================
# RePlayOS Installation
# ============================================================================

install_replayos() {
    info "Installing Zaparoo Core for RePlayOS..."

    local archive_name download_url
    archive_name="zaparoo-replayos_arm64-${VERSION}.tar.gz"
    download_url="${BASE_URL}/download/${VERSION_TAG}/${archive_name}"

    info "Downloading Zaparoo Core ${VERSION}..."
    info "URL: ${download_url}"

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would download: ${archive_name}"
        info "[DRY-RUN] Would create: /media/sd/zaparoo/"
        info "[DRY-RUN] Would install binary to: /media/sd/zaparoo/zaparoo"
        info "[DRY-RUN] Would run: /media/sd/zaparoo/zaparoo -install"
        success "[DRY-RUN] RePlayOS installation simulated"
        return 0
    fi

    # Download and extract
    download_and_extract "replayos"

    # Install binary
    info "Installing to /media/sd/zaparoo/..."
    if ! mkdir -p /media/sd/zaparoo; then
        abort "Failed to create /media/sd/zaparoo"
    fi

    if ! cp "${ZAPAROO_BIN}" /media/sd/zaparoo/zaparoo; then
        abort "Failed to copy binary to /media/sd/zaparoo/zaparoo"
    fi

    chmod +x /media/sd/zaparoo/zaparoo
    success "Installed to /media/sd/zaparoo/zaparoo"

    # Install and start the systemd service
    info "Installing systemd service..."
    if ! /media/sd/zaparoo/zaparoo -install; then
        abort "Failed to install systemd service"
    fi

    success "RePlayOS installation complete!"
    info "Run via SSH: /media/sd/zaparoo/zaparoo"
}

# ============================================================================
# Batocera Installation
# ============================================================================

install_batocera() {
    info "Installing Zaparoo Core for Batocera..."

    # Check for existing installations and clean up manual installs if needed
    local has_pacman_install=false
    local has_manual_install=false

    # Detect pacman installation
    if pacman -Q zaparoo-core >/dev/null 2>&1 || \
       ls /userdata/system/pacman/batoexec/zaparoo-core_* >/dev/null 2>&1; then
        has_pacman_install=true
        info "Existing pacman installation detected"
    fi

    # Detect manual installation (only if no pacman install)
    if [ "$has_pacman_install" = false ] && [ -f /userdata/system/zaparoo ]; then
        has_manual_install=true
        info "Manual installation detected - cleanup required"
    fi

    # Clean up manual installation if detected
    if [ "$has_manual_install" = true ]; then
        info "Cleaning up manual installation files..."

        if [ "$DRY_RUN" = true ]; then
            info "[DRY-RUN] Would stop and disable service: zaparoo_service"
            info "[DRY-RUN] Would remove: /userdata/system/zaparoo"
            info "[DRY-RUN] Would remove: /userdata/system/zaparoo_write_game.sh"
            info "[DRY-RUN] Would remove: /userdata/system/services/zaparoo_service"
            info "[DRY-RUN] Would remove: /userdata/roms/ports/Zaparoo.sh"
            info "[DRY-RUN] Would remove: /userdata/system/configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh"
        else
            # Stop and disable service if running
            batocera-services stop zaparoo_service 2>/dev/null || true
            batocera-services disable zaparoo_service 2>/dev/null || true

            # Remove manual installation files
            rm -f /userdata/system/zaparoo
            rm -f /userdata/system/zaparoo_write_game.sh
            rm -f /userdata/system/services/zaparoo_service
            rm -f /userdata/roms/ports/Zaparoo.sh

            # Remove manual-only configuration files
            rm -f /userdata/system/configs/emulationstation/scripts/game-selected/zaparoo_game_select.sh

            success "Manual installation cleaned up"
        fi
    fi

    # Build package filename
    local package_name download_url
    package_name="zaparoo-core-${VERSION}-1-any.pkg.tar.zst"
    download_url="${BASE_URL}/download/${VERSION_TAG}/${package_name}"

    info "Downloading Batocera package ${VERSION}..."
    info "URL: ${download_url}"

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would download: ${package_name}"
        info "[DRY-RUN] Would install with: pacman -U --noconfirm ${package_name}"
        info "[DRY-RUN] Package would install to:"
        info "[DRY-RUN]   - /userdata/system/zaparoo (wrapper + arch-specific binaries)"
        info "[DRY-RUN]   - /userdata/system/services/zaparoo_service"
        info "[DRY-RUN]   - /userdata/roms/ports/Zaparoo.sh"
        success "[DRY-RUN] Batocera package installation simulated successfully"
        return 0
    fi

    # Reuse the installer transaction directory.
    ensure_tmp_dir
    TMP_PACKAGE="${TMP_DIR}/${package_name}"

    # Download package
    if ! curl --fail --progress-bar --location "${download_url}" -o "${TMP_PACKAGE}"; then
        abort "Failed to download from ${download_url}"
    fi

    success "Downloaded ${package_name}"

    # Check if pacman is available
    if ! command -v pacman >/dev/null 2>&1; then
        abort "pacman is required for Batocera installation but not found"
    fi

    # Install package (suppress output to avoid confusing warnings from upstream bug)
    info "Installing package with pacman..."
    if ! pacman -U --noconfirm "${TMP_PACKAGE}" >/dev/null 2>&1; then
        abort "Failed to install package with pacman"
    fi

    success "Batocera package installed successfully"
}

# ============================================================================
# macOS Installation
# ============================================================================

install_macos() {
    # TODO: Implement macOS installation
    # - Download macOS binary
    # - Install to /usr/local/bin or ~/Applications
    # - Handle launchd service (instead of systemd)
    # - Install to /Applications for GUI apps

    abort "macOS installation not yet implemented. Please download manually from https://zaparoo.org/download"
}

# ============================================================================
# Windows Installation
# ============================================================================

install_windows() {
    abort "Windows installation requires the .exe installer. Please download it from https://zaparoo.org/download"
}

# ============================================================================
# Cleanup
# ============================================================================

cleanup() {
    if [ -n "${TMP_DIR:-}" ] && [ -d "${TMP_DIR}" ]; then
        rm -rf "${TMP_DIR}"
    fi
}

# ============================================================================
# Main Installation
# ============================================================================

status_linux_generic() {
    local current
    current="$(installed_version)"
    info "Zaparoo status"
    printf "  Binary: %s\n" "${APP_PATH}"
    printf "  Installed version: %s\n" "${current:-not installed}"
    if command -v systemctl >/dev/null 2>&1 && systemctl --user is-active --quiet zaparoo.service 2>/dev/null; then
        printf "  Service active: yes\n"
    else
        printf "  Service active: no\n"
    fi
}

uninstall_linux_generic() {
    local response
    if [ ! -x "${APP_PATH}" ]; then
        warn "Zaparoo application binary is not installed"
        return 0
    fi
    "${APP_PATH}" -uninstall service || warn "Failed to remove service"
    "${APP_PATH}" -uninstall desktop || warn "Failed to remove desktop shortcut"
    response="$(prompt_yes_no "Remove hardware support (requires sudo)?" "n")"
    if [ "${response}" = "y" ]; then
        sudo "${APP_PATH}" -uninstall hardware || warn "Failed to remove hardware support"
    fi
    "${APP_PATH}" -uninstall application || abort "Failed to remove application"
    success "Zaparoo Core removed; user data was preserved"
}

print_help() {
    cat <<EOF
Zaparoo Core Installer v${INSTALLER_VERSION}

Usage:
    install.sh [install|repair|status|uninstall] [options]
    curl -fsSL https://zaparoo.org/install.sh | bash

Options:
    -y, --yes              Accept defaults, don't prompt
    --channel stable|beta  Select release channel (default: stable)
    --dry-run              Show changes without applying them
    -h, --help             Show this help message
    -V, --version          Show installer version

Environment:
    ZAPAROO_VERSION        Install an exact signed release version

Modes:
    install     Install or update Zaparoo Core (default); optionally install Decky integration on SteamOS
    repair      Reinstall SteamOS integration files without replacing Core
    status      Report binary, service, Runtime, and API state
    uninstall   Remove application files while preserving user data
EOF
}

parse_args() {
    local mode_seen=false
    while [ $# -gt 0 ]; do
        case "$1" in
            install|repair|status|uninstall)
                if [ "${mode_seen}" = true ]; then
                    abort "Only one installer mode may be specified"
                fi
                MODE="$1"
                mode_seen=true
                shift
                ;;
            -y|--yes)
                NONINTERACTIVE=1
                shift
                ;;
            --channel)
                [ $# -ge 2 ] || abort "--channel requires stable or beta"
                CHANNEL="$2"
                shift 2
                ;;
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            -V|--version)
                echo "Zaparoo Core Installer v${INSTALLER_VERSION}"
                exit 0
                ;;
            -h|--help)
                print_help
                exit 0
                ;;
            *)
                abort "Unknown option: $1 (use -h for help)"
                ;;
        esac
    done
    case "${CHANNEL}" in
        stable|beta) ;;
        *) abort "Invalid channel: ${CHANNEL} (expected stable or beta)" ;;
    esac
}

main() {
    local os_type distro

    parse_args "$@"

    printf "\n"
    info "Zaparoo Core Installer (${MODE})"
    if [ "$DRY_RUN" = true ]; then
        warn "DRY-RUN MODE - No changes will be made"
    fi
    printf "\n"

    os_type="$(detect_os)"
    if [ -n "${CI:-}" ] && [ -z "${NONINTERACTIVE:-}" ]; then
        warn "Running in non-interactive mode because CI environment detected"
        NONINTERACTIVE=1
    fi
    if [ -n "${NONINTERACTIVE:-}" ]; then
        info "Running in non-interactive mode"
    fi

    check_requirements
    if [ "${MODE}" = "install" ]; then
        resolve_version
    fi

    if [ "${os_type}" = "linux" ]; then
        distro="$(detect_linux_distro)"
        case "${MODE}" in
            status)
                if [ "${distro}" = "steamos" ]; then
                    status_steamos
                else
                    status_linux_generic
                fi
                return
                ;;
            uninstall)
                if [ "${distro}" = "steamos" ]; then
                    uninstall_steamos
                else
                    uninstall_linux_generic
                fi
                return
                ;;
            repair)
                if [ "${distro}" != "steamos" ]; then
                    abort "Repair mode is currently supported only on SteamOS"
                fi
                repair_steamos
                return
                ;;
        esac
    fi

    case "${os_type}" in
        linux) install_linux_generic ;;
        macos) install_macos ;;
        windows) install_windows ;;
        *) abort "Unsupported operating system: ${os_type}. Please visit https://zaparoo.org/download for manual installation." ;;
    esac

    printf "\n"
    if [ "$DRY_RUN" = true ]; then
        success "Dry-run completed successfully!"
        info "No changes were made"
    else
        success "Zaparoo Core ${MODE} completed successfully!"
        printf "\n"
        info "Quick start:"
        printf "  %szaparoo%s          - Start Zaparoo Core\n" "${BOLD}" "${RESET}"
        printf "  %szaparoo -help%s    - Show help\n" "${BOLD}" "${RESET}"
    fi
    printf "\n"
    info "For more information, visit: https://zaparoo.org"
    printf "\n"
}

if [[ "${BASH_SOURCE[0]:-$0}" == "$0" ]]; then
    trap cleanup EXIT INT TERM
    main "$@"
fi
