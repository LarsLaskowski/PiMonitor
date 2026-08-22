#!/usr/bin/env bash
# Download and unpack the latest PiMonitor release for a Raspberry Pi.
#
# Usage:
#   ./download-latest.sh
#   ARCH=arm64 ./download-latest.sh  # optional manual override

set -euo pipefail

readonly REPOSITORY='larslaskowski/pimonitor'
# Restrict both the initial request and any redirect to HTTPS, so a
# compromised or misconfigured server can't downgrade the download to plain
# HTTP.
readonly CURL=(curl -fsSL --proto '=https' --proto-redir '=https')

if [[ -z "${ARCH:-}" ]]; then
  case "$(uname -m)" in
    aarch64|arm64)
      ARCH='arm64'
      ;;
    armv6l|armv7l|armv8l)
      # The armv6 binary also runs on newer 32-bit ARM Raspberry Pi OS.
      ARCH='armv6'
      ;;
    *)
      printf 'Could not determine a supported Raspberry Pi architecture from: %s\n' "$(uname -m)" >&2
      printf 'Set ARCH manually to arm64 or armv6.\n' >&2
      exit 2
      ;;
  esac
fi

case "${ARCH:-}" in
  arm64|armv6)
    ;;
  *)
    printf 'Unsupported ARCH %q; use arm64 or armv6.\n' "$ARCH" >&2
    exit 2
    ;;
esac

for command in curl tar; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'Required command not found: %s\n' "$command" >&2
    exit 1
  fi
done

version=$("${CURL[@]}" "https://api.github.com/repos/${REPOSITORY}/releases/latest" \
  | awk -F '"' '/"tag_name"/ { print $4; exit }')

if [[ -z "$version" ]]; then
  printf 'Could not determine the latest PiMonitor release version.\n' >&2
  exit 1
fi

archive="pimonitor_${version#v}_linux_${ARCH}.tar.gz"
directory="${archive%.tar.gz}"
url="https://github.com/${REPOSITORY}/releases/download/${version}/${archive}"

if [[ -e "$directory" ]]; then
  printf 'Target directory already exists: %s\n' "$directory" >&2
  printf 'Remove it or run the script in another directory.\n' >&2
  exit 1
fi

printf 'Downloading PiMonitor %s for %s ...\n' "$version" "$ARCH"
"${CURL[@]}" -o "$archive" "$url"

printf 'Extracting %s ...\n' "$archive"
tar xzf "$archive"

if [[ ! -d "$directory" ]]; then
  printf 'Expected extracted directory not found: %s\n' "$directory" >&2
  exit 1
fi

cd "$directory"
ls

printf '\nRelease extracted to %s\n' "$PWD"
printf 'To continue, run: cd %q && sudo ./install.sh\n' "$PWD"
