#!/bin/sh
# Collects the licence of every module linked into the binary into one file.
#
# This is not paperwork: the dependency tree carries Apache-2.0 code, and
# section 4 of that licence requires the NOTICE files of what you redistribute
# to travel with it. The packages ship a binary containing that code, so they
# ship this file next to the licence of hublot itself.
#
# Usage: scripts/third-party-licenses.sh <output file>
set -eu

out=${1:-dist/THIRD-PARTY-LICENSES.txt}
mkdir -p "$(dirname "$out")"

{
  echo "Third-party licences for hublot"
  echo
  echo "hublot itself is MIT licensed; see the LICENSE file next to this one."
  echo "The binary links the modules below, each under its own licence, which is"
  echo "reproduced here in full along with any NOTICE file it carries."
  echo
} > "$out"

# The modules that actually end up in the binary, not the whole module graph:
# test-only and tool dependencies are not redistributed.
go list -deps -f '{{if .Module}}{{.Module.Path}}	{{.Module.Dir}}{{end}}' ./cmd/hublot |
  grep -v '^github.com/RobinHil/hublot' |
  sort -u |
  while IFS='	' read -r module dir; do
    [ -n "$dir" ] || continue

    file=""
    for candidate in LICENSE LICENSE.txt LICENSE.md LICENCE COPYING COPYING.txt; do
      if [ -f "$dir/$candidate" ]; then
        file="$dir/$candidate"
        break
      fi
    done

    if [ -z "$file" ]; then
      echo "no licence file found for $module" >&2
      exit 1
    fi

    {
      echo "================================================================"
      echo "$module"
      echo "================================================================"
      echo
      cat "$file"
      echo
      if [ -f "$dir/NOTICE" ]; then
        echo "---- NOTICE ----"
        echo
        cat "$dir/NOTICE"
        echo
      fi
    } >> "$out"
  done

echo "wrote $out"
