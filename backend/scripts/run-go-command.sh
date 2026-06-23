#!/bin/sh
set -eu

if [ "$#" -lt 2 ]; then
    echo "usage: $0 <binary-name> <package> [args...]" >&2
    exit 2
fi

binary_name=$1
package=$2
shift 2

mkdir -p .bin
binary_path=".bin/$binary_name"

go build -o "$binary_path" "$package"
exec "./$binary_path" "$@"
