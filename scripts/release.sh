#!/bin/sh

# Cut a release: sync the add-on manifest version, push main, push the tag.
#
# Usage: scripts/release.sh <version>
#
# <version> is X.Y.Z (a leading "v" is accepted and stripped). The release
# workflow only accepts tags matching ^vX.Y.Z$ and requires the tag version
# to equal version: in gbbconnect_go/config.yaml, so this script keeps the
# two in lockstep and refuses anything the pipeline would reject.

set -eu

project_dir=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
manifest="$project_dir/gbbconnect_go/config.yaml"
changelog="$project_dir/gbbconnect_go/CHANGELOG.md"

version=${1:-}
version=${version#v}
if [ -z "$version" ]; then
	echo "usage: $0 <version>  (example: $0 0.1.4)" >&2
	exit 2
fi
if ! printf '%s' "$version" | grep -Eq '^[0-9]+\.[0-9]+\.[0-9]+$'; then
	echo "version must be X.Y.Z (release workflow rejects suffixes like -dev+1), got: $version" >&2
	exit 2
fi
tag=v$version

branch=$(git -C "$project_dir" symbolic-ref --short HEAD)
if [ "$branch" != "main" ]; then
	echo "must be on main, currently on: $branch" >&2
	exit 1
fi

if ! git -C "$project_dir" diff --quiet || ! git -C "$project_dir" diff --cached --quiet; then
	echo "working tree has uncommitted changes; commit or stash them first" >&2
	exit 1
fi

echo "Fetching origin"
git -C "$project_dir" fetch origin --tags

if [ "$(git -C "$project_dir" rev-parse HEAD)" != "$(git -C "$project_dir" rev-parse origin/main)" ]; then
	echo "main is not in sync with origin/main; pull or push first" >&2
	exit 1
fi

if git -C "$project_dir" rev-parse -q --verify "refs/tags/$tag" >/dev/null; then
	echo "tag $tag already exists" >&2
	exit 1
fi
if [ -n "$(git -C "$project_dir" ls-remote --tags origin "refs/tags/$tag")" ]; then
	echo "tag $tag already exists on origin" >&2
	exit 1
fi

if ! grep -q "^## $version\$" "$changelog"; then
	echo "no \"## $version\" entry in gbbconnect_go/CHANGELOG.md; write the changelog first" >&2
	exit 1
fi

current=$(awk -F '"' '/^version: / { print $2; exit }' "$manifest")
if [ "$current" = "$version" ]; then
	echo "gbbconnect_go/config.yaml already at $version"
else
	echo "Bumping gbbconnect_go/config.yaml: $current -> $version"
	awk -v version="$version" '
		!done && /^version: / { print "version: \"" version "\""; done = 1; next }
		{ print }
	' "$manifest" >"$manifest.tmp"
	mv "$manifest.tmp" "$manifest"

	# Re-read with the exact parser the release workflow's prepare job uses,
	# so anything it would reject (missing quotes, wrong line) fails here.
	written=$(awk -F '"' '/^version: / { print $2; exit }' "$manifest")
	if [ "$written" != "$version" ]; then
		git -C "$project_dir" checkout -- gbbconnect_go/config.yaml
		echo "manifest edit failed: workflow parser reads \"$written\", expected \"$version\"" >&2
		exit 1
	fi

	echo "Running add-on manifest tests"
	go -C "$project_dir" test ./internal/config >/dev/null

	git -C "$project_dir" add gbbconnect_go/config.yaml
	git -C "$project_dir" commit -m "Release $version"
fi

echo "Pushing main"
git -C "$project_dir" push origin main

echo "Tagging $tag"
git -C "$project_dir" tag -a "$tag" -m "Release $version"

echo "Pushing $tag"
git -C "$project_dir" push origin "$tag"

echo "Done: $tag pushed; the Release workflow takes it from here."
