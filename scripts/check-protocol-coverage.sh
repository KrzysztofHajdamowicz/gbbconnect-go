#!/bin/sh

set -eu

threshold=${PROTOCOL_COVERAGE_MIN:-85.0}
temporary_dir=$(mktemp -d "${TMPDIR:-/tmp}/gbbconnect-coverage.XXXXXX")
trap 'rm -rf "$temporary_dir"' EXIT HUP INT TERM

packages="
./internal/modbus
./internal/driver/solarmanv5
./internal/driver/modbustcp
./internal/protocol
"

for package in $packages; do
	profile_name=$(printf '%s' "$package" | tr '/.' '__')
	profile="$temporary_dir/$profile_name.out"
	go test "$package" -covermode=atomic -coverprofile="$profile" -count=1
	coverage=$(
		go tool cover -func="$profile" |
			awk '$1 == "total:" { gsub(/%/, "", $3); print $3 }'
	)
	if [ -z "$coverage" ]; then
		echo "could not determine coverage for $package" >&2
		exit 1
	fi

	printf '%-42s %6.1f%% (minimum %.1f%%)\n' \
		"$package" \
		"$coverage" \
		"$threshold"
	if ! awk -v actual="$coverage" -v minimum="$threshold" \
		'BEGIN { exit !(actual + 0 >= minimum + 0) }'
	then
		echo "$package coverage is below the required threshold" >&2
		exit 1
	fi
done
