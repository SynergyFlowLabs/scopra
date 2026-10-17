#!/bin/sh
# Scopra installer bootstrap.
#
#   curl -fsSL https://scopra-six.vercel.app/install.sh | sh
#
# Downloads the release archive for this machine, verifies its SHA-256 against
# the release's checksums.txt, unpacks it into a temporary directory, and hands
# the terminal to `scopra install`, which moves the binaries into place.
#
# Environment:
#   SCOPRA_VERSION   a release tag such as v0.1.0-alpha.1 (default: latest)
#
# The script is POSIX sh: it runs under dash, ash, and busybox. It never uses
# sudo, never writes outside the temporary directory and the install directory
# the interactive installer asks for, and never evaluates downloaded text.
set -eu

repository="SynergyFlowLabs/scopra"
releases="https://github.com/${repository}/releases"
version="${SCOPRA_VERSION:-latest}"

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

archive="scopra_${os}_${arch}.tar.gz"
if [ "$version" = latest ]; then
	base="${releases}/latest/download"
else
	base="${releases}/download/${version}"
fi

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

workdir=$(mktemp -d "${TMPDIR:-/tmp}/scopra-install.XXXXXX") || die "cannot create a temporary directory"
cleanup() { rm -rf "$workdir"; }
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM
trap 'exit 129' HUP

say "Downloading Scopra for ${os}/${arch} (${version})…"
download "${base}/checksums.txt" "${workdir}/checksums.txt" || die "cannot download checksums.txt; the release may not exist"
download "${base}/${archive}" "${workdir}/${archive}" || die "cannot download ${archive}"

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
