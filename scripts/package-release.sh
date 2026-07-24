#!/bin/sh

set -eu

if [ "$#" -ne 3 ]; then
	echo "usage: $0 <version> <binary-directory> <output-directory>" >&2
	exit 2
fi

version=$1
binary_dir=$2
output_dir=$3

for binary in \
	gbbconnect_linux_amd64 \
	gbbconnect_linux_arm64 \
	gbbconnect_linux_arm_v7 \
	gbbconnect_windows_amd64.exe \
	gbbconnect_darwin_arm64
do
	if [ ! -f "$binary_dir/$binary" ]; then
		echo "missing release binary: $binary_dir/$binary" >&2
		exit 1
	fi
done

mkdir -p "$output_dir"
output_dir=$(CDPATH= cd -- "$output_dir" && pwd)
staging_dir=$(mktemp -d "${TMPDIR:-/tmp}/gbbconnect-release.XXXXXX")
trap 'rm -rf "$staging_dir"' EXIT HUP INT TERM

package_tar() {
	source=$1
	target=$2
	archive_root="gbbconnect_${version}_${target}"

	mkdir "$staging_dir/$archive_root"
	cp "$binary_dir/$source" "$staging_dir/$archive_root/gbbconnect"
	chmod 0755 "$staging_dir/$archive_root/gbbconnect"
	tar -C "$staging_dir" -czf "$output_dir/$archive_root.tar.gz" "$archive_root"
	rm -rf "$staging_dir/$archive_root"
}

package_zip() {
	source=$1
	target=$2
	archive_root="gbbconnect_${version}_${target}"

	mkdir "$staging_dir/$archive_root"
	cp "$binary_dir/$source" "$staging_dir/$archive_root/gbbconnect.exe"
	chmod 0755 "$staging_dir/$archive_root/gbbconnect.exe"
	(
		cd "$staging_dir"
		zip -q -r "$output_dir/$archive_root.zip" "$archive_root"
	)
	rm -rf "$staging_dir/$archive_root"
}

package_tar gbbconnect_linux_amd64 linux_amd64
package_tar gbbconnect_linux_arm64 linux_arm64
package_tar gbbconnect_linux_arm_v7 linux_arm_v7
package_zip gbbconnect_windows_amd64.exe windows_amd64
package_tar gbbconnect_darwin_arm64 darwin_arm64

(
	cd "$output_dir"
	if command -v sha256sum >/dev/null 2>&1; then
		sha256sum ./*.tar.gz ./*.zip > SHA256SUMS
	else
		shasum -a 256 ./*.tar.gz ./*.zip > SHA256SUMS
	fi
)
