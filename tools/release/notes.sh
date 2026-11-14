#!/bin/sh
# Jana <edisi>-notes.json daripada RELEASE_NOTES.json (akar repo) untuk halaman Kemas kini.
#   sh tools/release/notes.sh VERSI FAIL_MUAT_TURUN > aw1000-notes.json
# "size" diisi dengan saiz fail yang dimuat turun (vpro.gz). Versi dalam RELEASE_NOTES.json mesti sama.
set -eu
VER=$1; F=$2
N=RELEASE_NOTES.json
[ -f "$N" ] || { echo "GAGAL: $N tiada" >&2; exit 1; }
grep -q "\"version\": *\"$VER\"" "$N" || { echo "GAGAL: kemas kini \"version\" dalam $N kepada $VER dahulu" >&2; exit 1; }
SZ=$(wc -c < "$F" | tr -d ' ')
sed "s/\"size\": *0/\"size\": $SZ/" "$N"
