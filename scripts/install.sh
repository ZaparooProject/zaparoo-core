#!/usr/bin/env bash
# Zaparoo Core Universal Installer
# Copyright (c) 2025 The Zaparoo Project Contributors.
# SPDX-License-Identifier: GPL-3.0-or-later

set -e          # Exit on error
set -o pipefail # Fail on pipeline errors
set -u          # Treat unset variables as errors

# ============================================================================
# Configuration
# ============================================================================

GITHUB_REPO="ZaparooProject/zaparoo-core"
DEFAULT_VERSION="2.7.0"  # Update this with each release
VERSION="${ZAPAROO_VERSION:-${DEFAULT_VERSION}}"
BASE_URL="https://github.com/${GITHUB_REPO}/releases"

# Dry-run mode (set by --dry-run flag)
DRY_RUN=false

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
    # Detect Linux distribution from /etc/os-release
    if [ ! -f /etc/os-release ]; then
        echo "generic"
        return
    fi

    # Source the os-release file to get distro info
    # shellcheck source=/dev/null
    . /etc/os-release

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

check_requirements() {
    info "Checking requirements..."

    if [ -z "${BASH_VERSION:-}" ]; then
        abort "Bash is required to run this installer"
    fi

    if ! command -v curl >/dev/null 2>&1; then
        abort "curl is required but not installed. Please install curl and try again."
    fi

    if ! command -v tar >/dev/null 2>&1; then
        abort "tar is required but not installed. Please install tar and try again."
    fi

    success "All requirements met"
}

# ============================================================================
# Download and Extract
# ============================================================================

download_and_extract() {
    local os_type arch archive_name download_url

    os_type="$1"
    arch="$(detect_arch)"

    info "Detected system: ${os_type}/${arch}"

    # Build filename with version and download URL
    archive_name="zaparoo-${os_type}_${arch}-${VERSION}.tar.gz"
    download_url="${BASE_URL}/download/v${VERSION}/${archive_name}"

    info "Downloading Zaparoo Core ${VERSION}..."
    info "URL: ${download_url}"

    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Would download: ${archive_name}"
        info "[DRY-RUN] Would extract to temporary directory"
        ZAPAROO_BIN="/tmp/zaparoo-dry-run"  # Dummy path for dry-run
        return 0
    fi

    # Create temp directory
    TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'zaparoo-install')"
    TMP_ARCHIVE="${TMP_DIR}/zaparoo.tar.gz"
    TMP_EXTRACT="${TMP_DIR}/extract"

    # Download archive
    if ! curl -fsSL "${download_url}" -o "${TMP_ARCHIVE}"; then
        abort "Failed to download from ${download_url}"
    fi

    success "Downloaded ${archive_name}"

    info "Extracting archive..."
    mkdir -p "${TMP_EXTRACT}"

    if ! tar -xzf "${TMP_ARCHIVE}" -C "${TMP_EXTRACT}"; then
        abort "Failed to extract archive"
    fi

    # Find the zaparoo binary
    ZAPAROO_BIN="$(find "${TMP_EXTRACT}" -type f -name "zaparoo" | head -n 1)"

    if [ -z "${ZAPAROO_BIN}" ] || [ ! -f "${ZAPAROO_BIN}" ]; then
        abort "Could not find zaparoo binary in archive"
    fi

    # Make it executable
    chmod +x "${ZAPAROO_BIN}"

    success "Extracted to temporary location"
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
        batocera)
            # Batocera has its own installation method
            install_batocera
            return 0
            ;;
        steamos|chimeraos)
            warn "Immutable filesystem detected (${distro})"
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

    success "Application installed to ~/.local/bin/zaparoo"
}

prompt_yes_no() {
    local prompt="$1"
    local default="${2:-n}"

    # Dry-run mode - always answer yes to show what would be installed
    if [ "$DRY_RUN" = true ]; then
        info "[DRY-RUN] Auto-answering prompt: yes"
        echo "y"
        return
    fi

    # Non-interactive mode
    if [ -n "${NONINTERACTIVE:-}" ]; then
        echo "${default}"
        return
    fi

    local yn
    if [ "${default}" = "y" ]; then
        printf "%s [Y/n] " "${prompt}"
    else
        printf "%s [y/N] " "${prompt}"
    fi

    read -r yn
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
    response="$(prompt_yes_no "Install systemd service (auto-start on login)?" "n")"

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

        success "Systemd service installed"
        info "Enable with: systemctl --user enable zaparoo"
        info "Start with: systemctl --user start zaparoo"
    else
        info "Skipping systemd service installation"
    fi
}

install_desktop() {
    local response
    response="$(prompt_yes_no "Install desktop shortcut?" "n")"

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
    local response
    response="$(prompt_yes_no "Install hardware support (udev rules, requires sudo)?" "n")"

    if [ "${response}" = "y" ]; then
        info "Installing hardware support (requires root)..."

        if [ "$DRY_RUN" = true ]; then
            info "[DRY-RUN] Would install udev rules to: /etc/udev/rules.d/99-zaparoo.rules"
            info "[DRY-RUN] Would reload udev with: udevadm control --reload-rules"
            info "[DRY-RUN] Would require: sudo privileges"
            success "[DRY-RUN] Hardware support installation simulated"
            return 0
        fi

        if ! sudo "${ZAPAROO_BIN}" -install hardware; then
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
    download_url="${BASE_URL}/download/v${VERSION}/${package_name}"

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

    # Create temp directory
    TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'zaparoo-install')"
    TMP_PACKAGE="${TMP_DIR}/${package_name}"

    # Download package
    if ! curl -fsSL "${download_url}" -o "${TMP_PACKAGE}"; then
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

trap cleanup EXIT INT TERM

# ============================================================================
# Main Installation
# ============================================================================

main() {
    local os_type

    # Parse arguments
    while [ $# -gt 0 ]; do
        case "$1" in
            --dry-run)
                DRY_RUN=true
                shift
                ;;
            *)
                abort "Unknown option: $1"
                ;;
        esac
    done

    # Print banner
    printf "\n"
    info "Zaparoo Core Installer"
    if [ "$DRY_RUN" = true ]; then
        warn "DRY-RUN MODE - No changes will be made"
    fi
    printf "\n"

    # Detect OS
    os_type="$(detect_os)"

    # Check for non-interactive mode
    if [ -n "${CI:-}" ] && [ -z "${NONINTERACTIVE:-}" ]; then
        warn "Running in non-interactive mode because CI environment detected"
        NONINTERACTIVE=1
    fi

    if [ -n "${NONINTERACTIVE:-}" ]; then
        info "Running in non-interactive mode"
    fi

    # Check requirements
    check_requirements

    # Route to appropriate installer based on OS
    case "${os_type}" in
        linux)
            install_linux_generic
            ;;
        macos)
            install_macos
            ;;
        windows)
            install_windows
            ;;
        *)
            abort "Unsupported operating system: ${os_type}. Please visit https://zaparoo.org/download for manual installation."
            ;;
    esac

    # Print success message
    printf "\n"
    if [ "$DRY_RUN" = true ]; then
        success "Dry-run completed successfully!"
        info "No changes were made to your system"
    else
        success "Zaparoo Core installed successfully!"
        printf "\n"
        info "Quick start:"
        printf "  %szaparoo%s          - Start Zaparoo Core\n" "${BOLD}" "${RESET}"
        printf "  %szaparoo -help%s    - Show help\n" "${BOLD}" "${RESET}"
    fi
    printf "\n"
    info "For more information, visit: https://zaparoo.org"
    printf "\n"
}

# Run main function
main "$@"

# Powered by Octocode MCP ⭐🐙 link:(https://github.com/bgauryy/octocode-mcp)
