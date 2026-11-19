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
repo() { # arch pkgdir : tulis susunan GitHub dalam aplsahaja/ (sama seperti akar repo)
  A=$1; P=$2
  if [ "$A" = armv7 ]; then
    cp "$P/new/vp" "$REPO/new/vp"; cp "$P/new/vpro" "$REPO/new/vpro"; gzip -9nc "$P/new/vpro" > "$REPO/new/vpro.gz"
    ( cd "$P/new" && sha256sum vpro vp ) > "$REPO/b618-sha256.txt"
    echo "$VER" > "$REPO/b618-latest.txt"
    "$B/relsign" -seed "$RELEASE_SEED" -in "$REPO/b618-sha256.txt" -out "$REPO/b618-sha256.txt.sig"
    sh tools/release/notes.sh "$VER" "$REPO/new/vpro.gz" > "$REPO/b618-notes.json"
  else
    I="$B/inst-$A"; mkdir -p "$I/vlesspro/bin"
    cp "$P/vlesspro/install.sh" "$P/vlesspro/vp" "$P/upgrade.sh" "$I/vlesspro/"; cp "$P/new/vpro" "$I/vlesspro/bin/vpro"
    chmod 755 "$I/vlesspro/vp" "$I/vlesspro/install.sh" "$I/vlesspro/bin/vpro"
    tar -C "$I" -czf "$REPO/aw1000/install.tgz" vlesspro
    cp "$P/new/vp" "$REPO/aw1000/vp"; cp "$P/new/vpro" "$REPO/aw1000/vpro"; gzip -9nc "$P/new/vpro" > "$REPO/aw1000/vpro.gz"
    { sha256sum "$P/new/vpro" | sed 's#  .*#  vpro#'; sha256sum "$P/new/vp" | sed 's#  .*#  vp#'; sha256sum "$REPO/aw1000/install.tgz" | sed 's#  .*#  install.tgz#'; } > "$REPO/aw1000-sha256.txt"
    echo "$VER" > "$REPO/aw1000-latest.txt"
    "$B/relsign" -seed "$RELEASE_SEED" -in "$REPO/aw1000-sha256.txt" -out "$REPO/aw1000-sha256.txt.sig"
    sh tools/release/notes.sh "$VER" "$REPO/aw1000/vpro.gz" > "$REPO/aw1000-notes.json"
  fi
}
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
  repo "$A" "$P"
  tar -C "$P" -czf "$OUT/vlesspro-$VER-APL-SAHAJA-$NM.tgz" .
  tar -C "$P" --exclude=./vlesspro -czf "$OUT/vlesspro-$VER-APL-SAHAJA-$NM-kemaskini.tgz" .
}
REPO=aplsahaja; rm -rf "$REPO"; mkdir -p "$REPO/new" "$REPO/aw1000"
mk armv7 arm 7 'armv7 (B618-65D)' vlesspro armv7-B618-65D
mk arm64 arm64 7 'arm64 (AW1000 dan AW1000 Ultra)' openwrt arm64-AW1000-AW1000Ultra
ls -la "$OUT"
