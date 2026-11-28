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
# --retry-all-errors makes curl retry on any transfer error (not just the
# default "clearly transient" subset), so a flaky connection that manifests
# as e.g. "curl: (23) Failed writing body" is retried automatically instead
# of requiring the user to re-run the script by hand.
readonly CURL=(curl -fsSL --proto '=https' --proto-redir '=https' --retry 3 --retry-delay 2 --retry-all-errors)

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

for command in curl tar sha256sum; do
  if ! command -v "$command" >/dev/null 2>&1; then
    printf 'Required command not found: %s\n' "$command" >&2
    exit 1
  fi
done

release_json=$("${CURL[@]}" "https://api.github.com/repos/${REPOSITORY}/releases/latest")
# Parse from the already-captured variable rather than piping curl straight
# into awk: awk's "exit" closes its end of the pipe as soon as it stops
# reading, which risks curl trying to write into an already-closed pipe
# (curl: (23) Failed writing body) if that ever happens before curl is done.
version=$(printf '%s\n' "$release_json" | awk -F '"' '/"tag_name"/ { print $4; exit }')

if [[ -z "$version" ]]; then
  printf 'Could not determine the latest PiMonitor release version.\n' >&2
  exit 1
fi

archive="pimonitor_${version#v}_linux_${ARCH}.tar.gz"
directory="${archive%.tar.gz}"
release_base="https://github.com/${REPOSITORY}/releases/download/${version}"
url="${release_base}/${archive}"

if [[ -e "$directory" ]]; then
  printf 'Target directory already exists: %s\n' "$directory" >&2
  printf 'Remove it or run the script in another directory.\n' >&2
  exit 1
fi

# Fetch the release's checksums.txt first and keep it in memory, so a missing
# or unusable entry stops the script before the archive is requested.
if ! checksums=$("${CURL[@]}" "${release_base}/checksums.txt"); then
  printf 'Could not download checksums.txt of PiMonitor %s.\n' "$version" >&2
  exit 1
fi
# A here-string, not a pipe, so awk closing its input early can't raise SIGPIPE
# under pipefail. Two matching lines join into a value the pattern rejects.
expected=$(awk -v name="$archive" '$2 == name { print $1 }' <<<"$checksums")
if [[ ! "$expected" =~ ^[0-9a-f]{64}$ ]]; then
  printf 'No checksum for %s in checksums.txt of PiMonitor %s.\n' "$archive" "$version" >&2
  exit 1
fi

printf 'Downloading PiMonitor %s for %s ...\n' "$version" "$ARCH"
if ! "${CURL[@]}" -o "$archive" "$url"; then
  rm -f -- "$archive"
  printf 'Could not download %s of PiMonitor %s.\n' "$archive" "$version" >&2
  exit 1
fi

actual=$(sha256sum -- "$archive")
actual=${actual%% *}
if [[ "$actual" != "$expected" ]]; then
  printf 'Checksum mismatch for %s: expected %s, got %s.\n' "$archive" "$expected" "$actual" >&2
  rm -f -- "$archive"
  printf 'Removed %s; nothing was extracted.\n' "$archive" >&2
  exit 1
fi
printf 'Checksum of %s verified.\n' "$archive"

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
