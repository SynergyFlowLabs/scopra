#!/usr/bin/env bash
# Builds the release archives that install.sh downloads.
#
#   scripts/release.sh v0.1.0-alpha.1 [--publish]
#
# With --publish the archives are copied into the documentation site under
# /releases/<tag>/ and /releases/latest.txt is updated. The site is served by
# Vercel from apps/docs-site/public, which is committed, so publishing is a copy
# followed by `make site` and a commit; the generator preserves `releases/`.
#
# Every archive contains scopra, scoprad, the licence, and the readme. The
# version is stamped into both binaries, so `scopra version` reports the tag
# rather than "dev". checksums.txt carries one SHA-256 line per archive and is
# uploaded beside them; install.sh refuses a release without a matching line.
#
# The archives are built without cgo and with -trimpath, so the same source
# produces the same bytes on any Linux machine with the same Go toolchain.
set -euo pipefail

version="${1:-}"
publish="${2:-}"
if [ -z "$version" ]; then
	echo "usage: scripts/release.sh vX.Y.Z [--publish]" >&2
	exit 2
fi
if [ -n "$publish" ] && [ "$publish" != "--publish" ]; then
	echo "unknown option: $publish" >&2
	exit 2
fi
case "$version" in
v*) ;;
*)
	echo "the version must start with v, for example v0.1.0-alpha.1" >&2
	exit 2
	;;
esac

root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$root"

out="${OUT:-dist}"
rm -rf "$out"
mkdir -p "$out"

ldflags="-s -w -X github.com/SynergyFlowLabs/scopra/internal/buildinfo.VersionOverride=${version}"
# Linux is the only supported platform: Mio confines commands with Linux
# namespaces and cgroups, so other archives would not run.
targets="${TARGETS:-linux/amd64 linux/arm64}"

for target in $targets; do
	os="${target%/*}"
	arch="${target#*/}"
	stage="$out/stage-${os}-${arch}"
	mkdir -p "$stage"
	for command in scopra scoprad; do
		CGO_ENABLED=0 GOOS="$os" GOARCH="$arch" GOTOOLCHAIN=local \
			go build -trimpath -ldflags "$ldflags" -o "$stage/$command" "./cmd/$command"
	done
	cp README.md LICENSE NOTICE "$stage/"
	archive="scopra_${os}_${arch}.tar.gz"
	tar --sort=name --owner=0 --group=0 --numeric-owner --mtime='UTC 2026-01-01' \
		-czf "$out/$archive" -C "$stage" scopra scoprad README.md LICENSE NOTICE
	rm -rf "$stage"
	echo "built $out/$archive"
done

# checksums.txt names the archives without a path, which is what install.sh
# matches against.
(
	cd "$out"
	sha256sum ./*.tar.gz | sed 's#\./##' >checksums.txt
)
echo "wrote $out/checksums.txt"
cat "$out/checksums.txt"

if [ "$publish" = "--publish" ]; then
	site="apps/docs-site/public/releases"
	mkdir -p "$site/$version"
	cp "$out"/*.tar.gz "$out/checksums.txt" "$site/$version/"
	printf '%s\n' "$version" >"$site/latest.txt"
	echo "published $version into $site"
	echo "next: make site, then commit apps/docs-site/public"
fi
