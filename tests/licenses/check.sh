#!/bin/sh
set -eu

repository_root=$(CDPATH='' cd -- "$(dirname -- "$0")/../.." && pwd -P)
cd "$repository_root"

module_lines=$(GOFLAGS=-mod=readonly go list -deps -test -f '{{with .Module}}{{if not .Main}}{{.Path}}|{{.Version}}|{{.Dir}}{{end}}{{end}}' ./... | sort -u)
missing_count=0
while IFS='|' read -r module_name module_version module_directory; do
	[ -n "$module_name" ] || continue
	license_file=$(find "$module_directory" -maxdepth 1 -type f \( \
		-iname 'license*' -o -iname 'copying*' -o -iname 'notice*' \
	\) -print -quit)
	if [ -z "$license_file" ]; then
		printf '%s\n' "missing dependency license: $module_name@$module_version" >&2
		missing_count=$((missing_count + 1))
	fi
done <<EOF
$module_lines
EOF

if [ "$missing_count" -ne 0 ]; then
	exit 1
fi
printf '%s\n' 'all dependency modules contain license or notice material'
