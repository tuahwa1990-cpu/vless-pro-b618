#!/bin/sh
# Edisi APL SAHAJA: tiada halaman web langsung dalam program. Hasil: dist/<versi>-apl-sahaja/ (tidak menyentuh fail repo, tidak push).
#   RELEASE_SEED=fail.seed RELEASE_PUB=BASE64 sh tools/release/make-aplsahaja.sh
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd); cd "$ROOT"
: "${RELEASE_SEED:?}"; : "${RELEASE_PUB:?}"
PUB="${VP_PUBKEY:-$(cat tools/issue-licence/LICENCE_PUB.txt)}"
VER=$(sed -n 's/^const version = "\(.*\)"/\1/p' src/main.go)
B=$(mktemp -d); trap 'rm -rf "$B"' EXIT
OUT="dist/$VER-apl-sahaja"; rm -rf "$OUT"; mkdir -p "$OUT"
(cd tools/release-sign && go build -o "$B/relsign" .)
cp upgrade.sh src/upgrade.sh
(cd src && go test -tags aplsahaja ./...) >/dev/null || { echo "GAGAL: ujian"; exit 1; }
mk() { # arch goarch goarm edisi vpdir installsrc nama
  A=$1; GA=$2; GM=$3; ED=$4; VD=$5; NM=$6
  (cd src && CGO_ENABLED=0 GOOS=linux GOARCH=$GA GOARM=$GM go build -tags aplsahaja -trimpath \
    -ldflags "-s -w -X main.pubkey=$PUB -X main.relpub=$RELEASE_PUB -X 'main.edisiNama=$ED'" -o "$B/vpro-$A" .)
  P="$B/pkg-$A"; mkdir -p "$P/vlesspro/bin" "$P/new"
  cp $VD/install.sh $VD/vp "$P/vlesspro/"; cp "$B/vpro-$A" "$P/vlesspro/bin/vpro"
  cp $VD/vp "$P/new/vp"; cp "$B/vpro-$A" "$P/new/vpro"; cp upgrade.sh "$P/"
  chmod 755 "$P/vlesspro/vp" "$P/vlesspro/install.sh" "$P/vlesspro/bin/vpro" "$P/new/vp" "$P/new/vpro" "$P/upgrade.sh"
  echo "$VER" > "$P/VERSION"
  ( cd "$P" && find . -type f | sed 's#^\./##' | sort | xargs sha256sum ) > "$P/SHA256SUMS"
  "$B/relsign" -seed "$RELEASE_SEED" -in "$P/SHA256SUMS" -out "$P/SHA256SUMS.sig"
  tar -C "$P" -czf "$OUT/vlesspro-$VER-APL-SAHAJA-$NM.tgz" .
  tar -C "$P" --exclude=./vlesspro -czf "$OUT/vlesspro-$VER-APL-SAHAJA-$NM-kemaskini.tgz" .
}
mk armv7 arm 7 'armv7 (B618-65D)' vlesspro armv7-B618-65D
mk arm64 arm64 7 'arm64 (AW1000 dan AW1000 Ultra)' openwrt arm64-AW1000-AW1000Ultra
ls -la "$OUT"
