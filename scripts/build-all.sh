#!/bin/sh

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
output_dir=${OUTPUT_DIR:-"$project_dir/dist"}
version=${VERSION:-}

if [ -z "$version" ]; then
	version=$(git -C "$project_dir" describe --tags --always --dirty 2>/dev/null || true)
fi
if [ -z "$version" ]; then
	version=dev
fi

mkdir -p "$output_dir"

build_target() {
	goos=$1
	goarch=$2
	goarm=$3
	output=$4

	echo "Building $output"
	if [ -n "$goarm" ]; then
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" GOARM="$goarm" \
			go build -trimpath -ldflags "-s -w -X main.version=$version" \
			-o "$output_dir/$output" ./cmd/gbbconnect
	else
		CGO_ENABLED=0 GOOS="$goos" GOARCH="$goarch" \
			go build -trimpath -ldflags "-s -w -X main.version=$version" \
			-o "$output_dir/$output" ./cmd/gbbconnect
	fi
}

cd "$project_dir"
build_target linux amd64 "" gbbconnect_linux_amd64
build_target linux arm64 "" gbbconnect_linux_arm64
build_target linux arm 7 gbbconnect_linux_arm_v7
build_target windows amd64 "" gbbconnect_windows_amd64.exe
build_target darwin arm64 "" gbbconnect_darwin_arm64

