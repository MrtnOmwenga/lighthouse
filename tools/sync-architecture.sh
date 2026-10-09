#!/bin/sh
# Copies a project's architecture definition into this site. The definition is written and tested
# in the project's own repository (docs/architecture.yaml there); this site only draws it.
#
#   tools/sync-architecture.sh <slug> <path to the project's checkout>
set -eu
slug=$1
repo=$2
out="$(dirname "$0")/../deploy/site/architecture/$slug.yaml"
commit=$(git -C "$repo" rev-parse --short HEAD)
if ! git -C "$repo" diff --quiet HEAD -- docs/architecture.yaml; then
  echo "docs/architecture.yaml has uncommitted changes in $repo" >&2
  exit 1
fi
{
  echo "# Copied from docs/architecture.yaml in the project's repository at commit $commit by"
  echo "# tools/sync-architecture.sh. Edit it there, where a test checks it against the code."
  git -C "$repo" show HEAD:docs/architecture.yaml
} > "$out"
echo "wrote $out from $commit"
