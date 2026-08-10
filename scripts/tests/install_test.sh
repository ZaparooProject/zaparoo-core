#!/usr/bin/env bash
# shellcheck disable=SC1091,SC2030,SC2031,SC2034,SC2329
# Test-local function overrides are invoked indirectly by sourced installer functions.
# Zaparoo Core installer regression tests.
# Copyright (c) 2026 The Zaparoo Project Contributors.
# SPDX-License-Identifier: GPL-3.0-or-later

set -euo pipefail

# Tests intentionally set globals consumed by sourced installer functions.
ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"
TEST_TMP="$(mktemp -d)"
trap 'rm -rf "${TEST_TMP}"' EXIT

export HOME="${TEST_TMP}/home"
mkdir -p "${HOME}"

# shellcheck source=../install.sh
source "${ROOT_DIR}/scripts/install.sh"

TESTS_RUN=0

fail() {
    printf 'not ok - %s\n' "$1" >&2
    exit 1
}

assert_equal() {
    local expected="$1"
    local actual="$2"
    local message="$3"
    if [ "${expected}" != "${actual}" ]; then
        fail "${message}: expected '${expected}', got '${actual}'"
    fi
}

assert_contains() {
    local value="$1"
    local expected="$2"
    local message="$3"
    if [[ "${value}" != *"${expected}"* ]]; then
        fail "${message}: '${value}' did not contain '${expected}'"
    fi
}

pass() {
    TESTS_RUN=$((TESTS_RUN + 1))
    printf 'ok %d - %s\n' "${TESTS_RUN}" "$1"
}

make_metadata_fakes() {
    local fixture_dir="$1"
    local fake_bin="$2"

    mkdir -p "${fixture_dir}" "${fake_bin}"
    cat > "${fake_bin}/curl" <<'EOF'
#!/usr/bin/env bash
set -euo pipefail
url=""
out=""
while [ $# -gt 0 ]; do
    case "$1" in
        -o)
            out="$2"
            shift 2
            ;;
        http://*|https://*)
            url="$1"
            shift
            ;;
        *) shift ;;
    esac
done
case "${url}" in
    https://api.github.com/*) cp "${FIXTURE_DIR}/release.json" "${out}" ;;
    *) cp "${FIXTURE_DIR}/$(basename "${url}")" "${out}" ;;
esac
EOF
    cat > "${fake_bin}/openssl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
    chmod +x "${fake_bin}/curl" "${fake_bin}/openssl"
}

test_parse_modes_and_channel() {
    MODE="install"
    CHANNEL="stable"
    parse_args repair --channel beta --yes
    assert_equal "repair" "${MODE}" "mode parsing"
    assert_equal "beta" "${CHANNEL}" "channel parsing"
    assert_equal "1" "${NONINTERACTIVE}" "non-interactive parsing"
    pass "parse installer mode and channel"
}

test_stdin_execution() {
    local output
    output="$(bash -s -- --version < "${ROOT_DIR}/scripts/install.sh")"
    assert_equal "Zaparoo Core Installer v2" "${output}" "stdin installer execution"
    pass "run installer from standard input"
}

test_inherited_tmp_dir_is_preserved() {
    local caller_tmp="${TEST_TMP}/caller-tmp"
    mkdir -p "${caller_tmp}"
    printf 'preserve' > "${caller_tmp}/sentinel"
    TMP_DIR="${caller_tmp}" bash "${ROOT_DIR}/scripts/install.sh" --version >/dev/null
    if [ ! -f "${caller_tmp}/sentinel" ]; then
        fail "installer removed inherited TMP_DIR"
    fi
    pass "preserve inherited temporary directory"
}

test_update_mode_is_rejected() {
    if (MODE="install"; parse_args update >/dev/null 2>&1); then
        fail "removed update mode was accepted"
    fi
    pass "reject removed update mode"
}

test_dry_run_prompts_accept() {
    local response
    DRY_RUN=true
    response="$(prompt_yes_no "Test prompt" "n" 2>/dev/null)"
    DRY_RUN=false
    assert_equal "y" "${response}" "dry-run prompt response"
    pass "auto-accept dry-run prompts"
}

test_signed_release_selection() {
    local fixture_dir="${TEST_TMP}/fixtures-selection"
    local fake_bin="${TEST_TMP}/bin-selection"
    make_metadata_fakes "${fixture_dir}" "${fake_bin}"

    printf 'archive checksums\n' > "${fixture_dir}/checksums.txt"
    printf 'signature' > "${fixture_dir}/checksums.txt.sig"
    printf '{\n  "tag_name": "v2.16.1"\n}\n' > "${fixture_dir}/release.json"

    export FIXTURE_DIR="${fixture_dir}"
    local old_path="${PATH}"
    PATH="${fake_bin}:${PATH}"
    TMP_DIR=""
    CHANNEL="stable"
    ZAPAROO_VERSION=""
    resolve_version >/dev/null
    assert_equal "2.16.1" "${VERSION}" "stable version selection"

    printf '{\n  "tag_name": "v2.17.0-beta.2"\n}\n' > "${fixture_dir}/release.json"
    TMP_DIR=""
    CHANNEL="beta"
    resolve_version >/dev/null
    assert_equal "2.17.0-beta.2" "${VERSION}" "beta version selection"

    TMP_DIR=""
    ZAPAROO_VERSION="v2.15.4"
    resolve_version >/dev/null
    assert_equal "2.15.4" "${VERSION}" "exact version override"
    PATH="${old_path}"
    unset FIXTURE_DIR ZAPAROO_VERSION
    pass "resolve stable, beta, and exact signed archive versions"
}

test_invalid_release_metadata() {
    local fixture_dir="${TEST_TMP}/fixtures-failure"
    local fake_bin="${TEST_TMP}/bin-failure"
    make_metadata_fakes "${fixture_dir}" "${fake_bin}"

    printf 'archive checksums\n' > "${fixture_dir}/checksums.txt"
    printf 'signature' > "${fixture_dir}/checksums.txt.sig"
    printf '{"tag_name":"not-a-version"}\n' > "${fixture_dir}/release.json"

    export FIXTURE_DIR="${fixture_dir}"
    if (PATH="${fake_bin}:${PATH}"; TMP_DIR=""; CHANNEL="stable"; resolve_version >/dev/null 2>&1); then
        fail "invalid release version was accepted"
    fi
    unset FIXTURE_DIR
    pass "reject invalid release metadata"
}

test_core_health_response() {
    core_health_ok "OK" || fail "plain-text Core health response was rejected"
    core_health_ok '{"status":"ok"}' || fail "JSON Core health response was rejected"
    if core_health_ok "not ok"; then
        fail "invalid Core health response was accepted"
    fi
    pass "accept current and legacy Core health responses"
}

test_api_identity_fields_are_literal() {
    local response='{"result":{"version":"2.17.0+build.1","platform":"steamos"}}'
    assert_equal "2.17.0+build.1" "$(json_string_field version "${response}")" \
        "literal API version"
    assert_equal "steamos" "$(json_string_field platform "${response}")" \
        "literal API platform"
    if [ "$(json_string_field version "${response}")" = "2x17y0+buildZ1" ]; then
        fail "API version punctuation was treated as a pattern"
    fi
    pass "compare API identity fields literally"
}

test_repair_requires_existing_install() {
    APP_PATH="${TEST_TMP}/missing/zaparoo"
    if (repair_steamos >/dev/null 2>&1); then
        fail "repair accepted a missing Core installation"
    fi
    pass "require installed Core for repair"
}

test_steamos_asset_name() {
    VERSION="2.16.1"
    VERSION_TAG="v2.16.1"
    DRY_RUN=true
    local output
    output="$(download_and_extract steamos)"
    assert_contains "${output}" "zaparoo-steamos_amd64-2.16.1.tar.gz" "SteamOS asset selection"
    DRY_RUN=false
    pass "select SteamOS release asset"
}

test_semver_comparison() {
    assert_equal "0" "$(semver_compare 2.17.0 2.17.0)" "equal stable versions"
    assert_equal "-1" "$(semver_compare 2.16.9 2.17.0)" "older stable version"
    assert_equal "1" "$(semver_compare 2.17.1 2.17.0)" "newer stable version"
    assert_equal "-1" "$(semver_compare 2.17.0-beta.2 2.17.0)" "prerelease before stable"
    assert_equal "1" "$(semver_compare 2.17.0-beta.10 2.17.0-beta.2)" "numeric prerelease order"
    assert_equal "0" "$(semver_compare 2.17.0+build.1 2.17.0+build.2)" "build metadata ignored"
    if semver_compare 46e9fbb7-dev 2.17.0 >/dev/null 2>&1; then
        fail "non-semver development build was compared as a release"
    fi
    assert_equal "-1" "$(upgrade_relation 46e9fbb7-dev 2.17.0)" \
        "development build replacement"
    pass "compare release versions and replace development builds"
}

test_decky_release_selection() {
    local metadata="${TEST_TMP}/decky-release.json"
    local output digest
    digest="$(printf 'archive' | sha256sum | awk '{ print $1 }')"
    cat > "${metadata}" <<EOF
{
  "tag_name": "v0.1.0",
  "draft": false,
  "prerelease": false,
  "assets": [{
    "name": "Zaparoo-v0.1.0.zip",
    "size": 7,
    "digest": "sha256:${digest}",
    "browser_download_url": "https://github.com/ZaparooProject/zaparoo-decky/releases/download/v0.1.0/Zaparoo-v0.1.0.zip"
  }]
}
EOF
    output="$(resolve_decky_release "${metadata}")"
    assert_contains "${output}" $'0.1.0\thttps://github.com/' "Decky release selection"

    printf '%s\n' '{"tag_name":"v0.1.0","draft":false,"prerelease":false,"assets":[]}' > "${metadata}"
    if resolve_decky_release "${metadata}" >/dev/null 2>&1; then
        fail "Decky release without package was accepted"
    fi
    pass "resolve bounded Decky release asset"
}

test_decky_archive_validation() {
    local archive="${TEST_TMP}/Zaparoo-v0.1.0.zip"
    local output="${TEST_TMP}/decky-output"
    python3 - "${archive}" <<'PY'
import json
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1], "w") as archive:
    archive.writestr("Zaparoo/LICENSE", "license")
    archive.writestr("Zaparoo/README.md", "readme")
    archive.writestr("Zaparoo/dist/index.js", "export default {}")
    archive.writestr("Zaparoo/main.py", "class Plugin: pass")
    archive.writestr("Zaparoo/package.json", json.dumps({"name": "zaparoo-decky", "version": "0.1.0"}))
    archive.writestr("Zaparoo/plugin.json", json.dumps({"name": "Zaparoo", "flags": []}))
PY
    extract_decky_archive "${archive}" "${output}" "0.1.0"
    if [ ! -f "${output}/Zaparoo/dist/index.js" ]; then
        fail "valid Decky archive was not extracted"
    fi

    python3 - "${archive}" <<'PY'
import sys
import zipfile

with zipfile.ZipFile(sys.argv[1], "a") as archive:
    archive.writestr("Zaparoo/../escape", "bad")
PY
    if extract_decky_archive "${archive}" "${output}-unsafe" "0.1.0" >/dev/null 2>&1; then
        fail "unsafe Decky archive path was accepted"
    fi
    pass "validate and safely extract Decky archive"
}

test_unrecognized_binary_is_not_replaced() {
    APP_PATH="${TEST_TMP}/unrecognized/zaparoo"
    mkdir -p "$(dirname "${APP_PATH}")"
    printf 'not core' > "${APP_PATH}"
    if (install_steamos_transaction >/dev/null 2>&1); then
        fail "unrecognized existing Core path was accepted"
    fi
    assert_equal "not core" "$(cat "${APP_PATH}")" "unrecognized binary preservation"
    rm "${APP_PATH}"
    ln -s "${TEST_TMP}/missing-target" "${APP_PATH}"
    if (install_steamos_transaction >/dev/null 2>&1); then
        fail "symbolic-link Core path was accepted"
    fi
    if [ ! -L "${APP_PATH}" ]; then
        fail "symbolic-link Core path was replaced"
    fi
    pass "refuse to replace unrecognized canonical binary"
}

test_decky_requires_compatible_core() {
    local output
    output="$(
        decky_is_installed() { return 0; }
        installed_version() { echo "2.16.1"; }
        offer_decky_plugin 2>&1
    )"
    assert_contains "${output}" "requires Core 2.17.0 or newer" "Decky minimum Core version"
    pass "skip Decky plugin for incompatible Core"
}

test_steamos_temporary_admin_lifecycle() {
    local password_input="${TEST_TMP}/temporary-password-input"
    local sudo_log="${TEST_TMP}/temporary-password-sudo.log"
    local output
    output="$(
        sudo() {
            printf '%s\n' "$*" >> "${sudo_log}"
            if [ "$1" = "-n" ] && [ "${2:-}" = "true" ]; then
                return 1
            fi
            if [[ " $* " == *" -S "* ]]; then
                cat >/dev/null || true
            fi
        }
        passwd() {
            if [ "$1" = "-S" ]; then
                echo "deck NP 2026-01-01 0 99999 7 -1"
                return
            fi
            cat > "${password_input}"
        }
        id() {
            case "$1" in
                -un) echo deck ;;
                *) command id "$@" ;;
            esac
        }
        prompt_yes_no() { echo y; }

        STEAMOS_ADMIN_DECLINED=false
        STEAMOS_TEMP_PASSWORD_SET=false
        STEAMOS_ADMIN_USER=""
        ensure_steamos_admin
        printf 'password_set=%s\n' "${STEAMOS_TEMP_PASSWORD_SET}"
        cleanup_steamos_admin
        printf 'password_removed=%s\n' "${STEAMOS_TEMP_PASSWORD_SET}"
    )"

    assert_contains "${output}" "password_set=true" "temporary password setup"
    assert_contains "${output}" "password_removed=false" "temporary password cleanup"
    assert_equal $'Zaparoo!\nZaparoo!' "$(cat "${password_input}")" "recoverable temporary password"
    assert_contains "$(cat "${sudo_log}")" "passwd -d deck" "temporary password removal"
    pass "set and remove recoverable SteamOS admin password"
}

test_gui_admin_password_is_reused_without_timestamp() {
    local sudo_input="${TEST_TMP}/gui-sudo-input"
    local sudo_log="${TEST_TMP}/gui-sudo.log"

    (
        zenity() { echo "existing-password"; }
        sudo() {
            printf '%s\n' "$*" >> "${sudo_log}"
            if [[ " $* " == *" -S "* ]]; then
                cat >> "${sudo_input}"
            fi
        }
        detect_linux_distro() { echo steamos; }

        STEAMOS_ADMIN_PASSWORD=""
        validate_gui_admin_password
        run_privileged echo protected
    )

    assert_contains "$(cat "${sudo_log}")" "-S -p  echo protected" \
        "GUI privileged command password reuse"
    assert_equal $'existing-password\nexisting-password' "$(cat "${sudo_input}")" \
        "GUI password input"
    pass "reuse GUI admin password without sudo timestamp"
}

test_steamos_admin_decline_is_remembered() {
    local password_log="${TEST_TMP}/declined-password.log"
    local output
    output="$(
        sudo() {
            if [ "$1" = "-n" ] && [ "${2:-}" = "true" ]; then
                return 1
            fi
            return 0
        }
        passwd() {
            if [ "$1" = "-S" ]; then
                echo "deck NP 2026-01-01 0 99999 7 -1"
            else
                printf 'changed\n' >> "${password_log}"
            fi
        }
        id() { echo deck; }
        prompt_yes_no() { echo n; }

        STEAMOS_ADMIN_DECLINED=false
        STEAMOS_TEMP_PASSWORD_SET=false
        if ensure_steamos_admin; then
            echo "unexpected-success"
        fi
        if ensure_steamos_admin; then
            echo "unexpected-repeat-success"
        fi
        printf 'declined=%s\n' "${STEAMOS_ADMIN_DECLINED}"
    )"

    assert_contains "${output}" "declined=true" "admin decline state"
    if [ -e "${password_log}" ]; then
        fail "declined admin access changed the password"
    fi
    pass "remember declined SteamOS admin access"
}

test_install_hardware_accepts_installed_binary() {
    local installed_binary="${TEST_TMP}/installed/zaparoo"
    local output
    output="$(
        unset ZAPAROO_BIN
        prompt_yes_no() { echo y; }
        sudo() { printf '%s\n' "$*"; }
        install_hardware "${installed_binary}"
    )"
    assert_contains "${output}" "${installed_binary} -install hardware" \
        "installed binary hardware setup"
    pass "use installed binary for hardware setup"
}

test_failed_install_removes_integration() {
    local fake_bin="${TEST_TMP}/bin-rollback"
    local rollback_log="${TEST_TMP}/rollback.log"
    mkdir -p "${fake_bin}" "${TEST_TMP}/install"
    cat > "${fake_bin}/systemctl" <<'EOF'
#!/usr/bin/env bash
exit 0
EOF
    chmod +x "${fake_bin}/systemctl"

    APP_PATH="${TEST_TMP}/install/zaparoo"
    cat > "${APP_PATH}" <<'EOF'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "${ROLLBACK_LOG}"
EOF
    chmod +x "${APP_PATH}"
    export ROLLBACK_LOG="${rollback_log}"
    PATH="${fake_bin}:${PATH}" rollback_steamos_transaction "" "${TEST_TMP}/missing-backup" >/dev/null 2>&1
    if [ -e "${APP_PATH}" ]; then
        fail "failed installation binary was not removed"
    fi
    local rollback_actions
    rollback_actions="$(cat "${rollback_log}")"
    assert_contains "${rollback_actions}" "-uninstall service" "service rollback"
    assert_contains "${rollback_actions}" "-uninstall steam-runtime" "Steam Runtime rollback"
    assert_contains "${rollback_actions}" "-uninstall desktop" "desktop rollback"
    assert_contains "${rollback_actions}" "-uninstall application" "application rollback"
    unset ROLLBACK_LOG
    pass "remove integration after failed fresh installation"
}

test_parse_modes_and_channel
test_stdin_execution
test_inherited_tmp_dir_is_preserved
test_update_mode_is_rejected
test_dry_run_prompts_accept
test_signed_release_selection
test_invalid_release_metadata
test_core_health_response
test_api_identity_fields_are_literal
test_repair_requires_existing_install
test_steamos_asset_name
test_semver_comparison
test_decky_release_selection
test_decky_archive_validation
test_unrecognized_binary_is_not_replaced
test_decky_requires_compatible_core
test_steamos_temporary_admin_lifecycle
test_gui_admin_password_is_reused_without_timestamp
test_steamos_admin_decline_is_remembered
test_install_hardware_accepts_installed_binary
test_failed_install_removes_integration

printf '1..%d\n' "${TESTS_RUN}"
