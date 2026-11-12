#!/bin/bash
# Installs the stack-neutral linters the analyzer gate runs on changed shell scripts, GitHub workflows and
# Dockerfiles (`.squad/tools/analyzer_common.py`): shellcheck, actionlint and hadolint, as pinned release
# binaries verified against the SHA-256 recorded here. Called by every profile's SessionStart hook; idempotent
# (a binary of the pinned version is kept), does nothing outside remote sessions, and never fails the session:
# a download that is refused (egress policy) or does not match its checksum is reported, and the gate then
# says "NOT RUN" for that tool.
#
# Updating a version: change the version and the two checksums (one per architecture) below; the
# checksums are those of the release assets on GitHub (actionlint and hadolint publish them next to the asset).
set -uo pipefail

if [[ "${CLAUDE_CODE_REMOTE:-}" != "true" ]]; then
  exit 0
fi

SHELLCHECK_VERSION="v0.11.0"
SHELLCHECK_SHA256_X86_64="8c3be12b05d5c177a04c29e3c78ce89ac86f1595681cab149b65b97c4e227198"
SHELLCHECK_SHA256_AARCH64="12b331c1d2db6b9eb13cfca64306b1b157a86eb69db83023e261eaa7e7c14588"
ACTIONLINT_VERSION="1.7.12"
ACTIONLINT_SHA256_X86_64="8aca8db96f1b94770f1b0d72b6dddcb1ebb8123cb3712530b08cc387b349a3d8"
ACTIONLINT_SHA256_AARCH64="325e971b6ba9bfa504672e29be93c24981eeb1c07576d730e9f7c8805afff0c6"
HADOLINT_VERSION="v2.14.0"
HADOLINT_SHA256_X86_64="6bf226944684f56c84dd014e8b979d27425c0148f61b3bd99bcc6f39e9dc5a47"
HADOLINT_SHA256_AARCH64="331f1d3511b84a4f1e3d18d52fec284723e4019552f4f47b19322a53ce9a40ed"

bin_dir="${HOME}/.local/bin"
mkdir -p "$bin_dir"
case ":${PATH}:" in
  *":${bin_dir}:"*) ;;
  *)
    export PATH="${bin_dir}:${PATH}"
    if [[ -n "${CLAUDE_ENV_FILE:-}" ]]; then
      echo "export PATH=\"\$HOME/.local/bin:\$PATH\"" >> "$CLAUDE_ENV_FILE"
    fi
    ;;
esac

# One checksum per tool for this machine; the release asset names differ per project (uname, Go and hadolint style).
case "$(uname -m)" in
  x86_64)
    arch="x86_64"; go_arch="amd64"; hadolint_arch="$arch"
    shellcheck_sha256="$SHELLCHECK_SHA256_X86_64"; actionlint_sha256="$ACTIONLINT_SHA256_X86_64"
    hadolint_sha256="$HADOLINT_SHA256_X86_64"
    ;;
  aarch64|arm64)
    arch="aarch64"; go_arch="arm64"; hadolint_arch="$go_arch"
    shellcheck_sha256="$SHELLCHECK_SHA256_AARCH64"; actionlint_sha256="$ACTIONLINT_SHA256_AARCH64"
    hadolint_sha256="$HADOLINT_SHA256_AARCH64"
    ;;
  *)
    echo "install-linters: unsupported architecture $(uname -m); shellcheck, actionlint and hadolint are not installed." >&2
    exit 0
    ;;
esac

# have <tool> <version-substring>: the installed binary already reports the pinned version.
have() {
  local tool="$1" wanted="$2"
  if command -v "$tool" >/dev/null 2>&1 && "$tool" --version 2>/dev/null | head -n 3 | grep -qF "$wanted"; then
    return 0
  fi
  return 1
}

# fetch <url> <sha256> <destination>: download to a temporary file, keep it only when the checksum matches.
fetch() {
  local url="$1" sha256="$2" destination="$3" tmp
  tmp="$(mktemp "${bin_dir}/download.XXXXXX")"
  if ! curl -fsSL --retry 3 --max-time 120 -o "$tmp" "$url"; then
    echo "install-linters: could not download ${url##*/} (egress policy?); the analyzer gate will report NOT RUN for it." >&2
    rm -f "$tmp"
    return 1
  fi
  if ! echo "${sha256}  ${tmp}" | sha256sum -c --quiet --status; then
    echo "install-linters: checksum mismatch for ${url##*/}; the file was discarded." >&2
    rm -f "$tmp"
    return 1
  fi
  mv -f "$tmp" "$destination" || return 1
  return 0
}

status=0

if ! have shellcheck "${SHELLCHECK_VERSION#v}"; then
  archive="${bin_dir}/shellcheck.tar.xz"
  if fetch "https://github.com/koalaman/shellcheck/releases/download/${SHELLCHECK_VERSION}/shellcheck-${SHELLCHECK_VERSION}.linux.${arch}.tar.xz" "$shellcheck_sha256" "$archive"; then
    tar -xJf "$archive" -C "$bin_dir" --strip-components=1 "shellcheck-${SHELLCHECK_VERSION}/shellcheck" \
      && chmod 0755 "${bin_dir}/shellcheck" || status=1
    rm -f "$archive"
  else
    status=1
  fi
fi

if ! have actionlint "$ACTIONLINT_VERSION"; then
  archive="${bin_dir}/actionlint.tar.gz"
  if fetch "https://github.com/rhysd/actionlint/releases/download/v${ACTIONLINT_VERSION}/actionlint_${ACTIONLINT_VERSION}_linux_${go_arch}.tar.gz" "$actionlint_sha256" "$archive"; then
    tar -xzf "$archive" -C "$bin_dir" actionlint && chmod 0755 "${bin_dir}/actionlint" || status=1
    rm -f "$archive"
  else
    status=1
  fi
fi

if ! have hadolint "${HADOLINT_VERSION#v}"; then
  if fetch "https://github.com/hadolint/hadolint/releases/download/${HADOLINT_VERSION}/hadolint-Linux-${hadolint_arch}" "$hadolint_sha256" "${bin_dir}/hadolint"; then
    chmod 0755 "${bin_dir}/hadolint"
  else
    status=1
  fi
fi

if [[ "$status" -ne 0 ]]; then
  echo "install-linters: not every linter is installed; see the messages above (.squad/stack.md, Analyzer gate)." >&2
fi
exit 0
