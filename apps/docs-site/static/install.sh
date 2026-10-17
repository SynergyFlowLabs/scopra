#!/bin/sh
# Scopra installer bootstrap.
#
#   curl -fsSL https://scopra-six.vercel.app/install.sh | sh
#
# Resolves the current release, downloads the archive for this machine, verifies
# its SHA-256 against the release's checksums.txt, unpacks it into a temporary
# directory, and hands the terminal to `scopra install`, which moves the
# binaries into place.
#
# Environment:
#   SCOPRA_VERSION   a release tag such as v0.1.0-alpha.1 (default: the version
#                    published in /releases/latest.txt)
#
# The script is POSIX sh: it runs under dash, ash, and busybox. It never uses
# sudo, never writes outside the temporary directory and the install directory
# the interactive installer asks for, and never evaluates downloaded text.
set -eu

site="https://scopra-six.vercel.app"
version="${SCOPRA_VERSION:-}"

say() { printf '%s\n' "$*" >&2; }
die() {
	printf 'scopra-install: %s\n' "$*" >&2
	exit 1
}

case "$(uname -s)" in
Linux) os=linux ;;
Darwin)
	say "Scopra runs on Linux."
	say "Its daemon confines every command with rootless Podman, Linux namespaces, and cgroups v2, so macOS is not supported yet."
	say "Nothing was changed."
	exit 1
	;;
*) die "unsupported operating system: $(uname -s)" ;;
esac

case "$(uname -m)" in
x86_64 | amd64) arch=amd64 ;;
aarch64 | arm64) arch=arm64 ;;
*) die "unsupported architecture: $(uname -m)" ;;
esac

fetch() {
	# fetch URL: print the body to stdout
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 60 "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -T 30 -O - "$1"
	else
		die "curl or wget is required to download Scopra"
	fi
}

download() {
	# download URL DESTINATION
	if command -v curl >/dev/null 2>&1; then
		curl -fsSL --retry 3 --retry-delay 1 --connect-timeout 15 --max-time 300 -o "$2" "$1"
	elif command -v wget >/dev/null 2>&1; then
		wget -q -T 30 -O "$2" "$1"
	else
		die "curl or wget is required to download Scopra"
	fi
}

sha256_of() {
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum "$1" | awk '{ print $1 }'
	elif command -v shasum >/dev/null 2>&1; then
		shasum -a 256 "$1" | awk '{ print $1 }'
	else
		die "sha256sum or shasum is required to verify the download"
	fi
}

if [ -z "$version" ]; then
	version=$(fetch "${site}/releases/latest.txt") || die "cannot read ${site}/releases/latest.txt"
fi
# The version names a path, so it is checked before it is used: one leading v,
# then only digits, letters, dots, dashes, underscores, and plus signs.
case "$version" in
v[0-9]*) ;;
*) die "unexpected release version: ${version}" ;;
esac
case "$version" in
*[!A-Za-z0-9._+-]*) die "unexpected release version: ${version}" ;;
esac
[ ${#version} -le 64 ] || die "unexpected release version: ${version}"

base="${site}/releases/${version}"
archive="scopra_${os}_${arch}.tar.gz"

workdir=$(mktemp -d "${TMPDIR:-/tmp}/scopra-install.XXXXXX") || die "cannot create a temporary directory"
cleanup() { rm -rf "$workdir"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

say "Downloading Scopra ${version} for ${os}/${arch}…"
download "${base}/checksums.txt" "${workdir}/checksums.txt" || die "cannot download ${version}/checksums.txt"
download "${base}/${archive}" "${workdir}/${archive}" || die "cannot download ${version}/${archive}"

# A release check that fails closed: exactly one checksum line must name this
# archive, and it must be a well-formed SHA-256 digest.
expected=$(awk -v name="$archive" '$2 == name { print $1 }' "${workdir}/checksums.txt")
[ -n "$expected" ] || die "checksums.txt has no entry for ${archive}"
case "$expected" in
*[!0-9a-f]*) die "the checksum for ${archive} is malformed" ;;
esac
[ ${#expected} -eq 64 ] || die "the checksum for ${archive} is not a SHA-256 digest"

actual=$(sha256_of "${workdir}/${archive}")
[ "$actual" = "$expected" ] || die "checksum mismatch for ${archive}: expected ${expected}, got ${actual}"

[ -s "${workdir}/${archive}" ] || die "${archive} is empty"
bytes=$(wc -c <"${workdir}/${archive}" | tr -d ' ')
[ "$bytes" -le 268435456 ] || die "${archive} is larger than expected"

mkdir -p "${workdir}/package"
tar -xzf "${workdir}/${archive}" -C "${workdir}/package" || die "cannot unpack ${archive}"
[ -x "${workdir}/package/scopra" ] || die "the archive does not contain an executable scopra"

status=0
"${workdir}/package/scopra" install "$@" || status=$?
exit "$status"
