#!/bin/sh
# Jana keluaran VLESS Pro untuk AW1000 Ultra (OpenWrt, arm64).
# Guna (dari akar repo, RELEASE_SEED/RELEASE_PUB sama seperti make-release.sh):
#   RELEASE_SEED=fail.seed RELEASE_PUB=BASE64 sh tools/release/make-aw1000.sh
# Hasil dalam dist/<versi>/: vlesspro-<versi>-aw1000.tgz (kemas kini dari UI).
# Fail repo: aw1000-latest.txt, aw1000-sha256.txt, aw1000/{vp,vpro,install.tgz,pasang.sh}.
# Tidak push apa-apa.
set -eu
ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"
: "${RELEASE_SEED:?Tetapkan RELEASE_SEED}"
: "${RELEASE_PUB:?Tetapkan RELEASE_PUB}"
PUB="${VP_PUBKEY:-$(cat tools/issue-licence/LICENCE_PUB.txt)}"
VER=$(sed -n 's/^const version = "\(.*\)"/\1/p' src/main.go)
[ -n "$VER" ] || { echo "GAGAL: versi tidak dijumpai"; exit 1; }
B=$(mktemp -d); trap 'rm -rf "$B"' EXIT
cp upgrade.sh src/upgrade.sh
echo "[1/5] Ujian unit ..."
(cd src && go test ./...) || { echo "GAGAL: ujian"; exit 1; }
echo "[2/5] Bina vpro arm64 ($VER) ..."
(cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm64 \
  go build -trimpath -ldflags "-s -w -X main.pubkey=$PUB -X main.relpub=$RELEASE_PUB" -o "$B/vpro" .)
echo "[3/5] Susun pakej ..."
P="$B/pkg"; I="$B/inst"
mkdir -p "$P/vlesspro/bin" "$P/new" "$I/vlesspro/bin"
for D in "$P" "$I"; do
  cp openwrt/install.sh openwrt/vp upgrade.sh "$D/vlesspro/"
  cp "$B/vpro" "$D/vlesspro/bin/vpro"
done
cp openwrt/vp "$P/new/vp"; cp "$B/vpro" "$P/new/vpro"; cp upgrade.sh "$P/"
chmod 755 "$P/vlesspro/vp" "$P/vlesspro/install.sh" "$P/vlesspro/bin/vpro" "$P/new/vp" "$P/new/vpro" "$P/upgrade.sh" "$I/vlesspro/vp" "$I/vlesspro/install.sh" "$I/vlesspro/bin/vpro"
echo "$VER" > "$P/VERSION"
echo "[4/5] Tandatangan ..."
( cd "$P" && find . -type f | sed 's#^\./##' | sort | xargs sha256sum ) > "$B/SHA256SUMS"
cp "$B/SHA256SUMS" "$P/SHA256SUMS"
(cd tools/release-sign && go build -o "$B/relsign" .)
"$B/relsign" -seed "$RELEASE_SEED" -in "$P/SHA256SUMS" -out "$P/SHA256SUMS.sig"
OUT="dist/$VER"; mkdir -p "$OUT" aw1000
tar -C "$P" -czf "$OUT/vlesspro-$VER-aw1000.tgz" .
tar -C "$P" --exclude=./vlesspro -czf "$OUT/vlesspro-$VER-aw1000-kemaskini.tgz" .
tar -C "$I" -czf aw1000/install.tgz vlesspro
cp openwrt/vp aw1000/vp; cp "$B/vpro" aw1000/vpro; gzip -9nc "$B/vpro" > aw1000/vpro.gz
echo "[5/5] Tulis fail repo ..."
{ sha256sum "$B/vpro" | sed 's#  .*#  vpro#'; sha256sum openwrt/vp | sed 's#  .*#  vp#'; sha256sum aw1000/install.tgz | sed 's#  .*#  install.tgz#'; } > aw1000-sha256.txt
echo "$VER" > aw1000-latest.txt
"$B/relsign" -seed "$RELEASE_SEED" -in aw1000-sha256.txt -out aw1000-sha256.txt.sig
sh tools/release/notes.sh "$VER" aw1000/vpro.gz > aw1000-notes.json
echo "Selesai: $OUT/vlesspro-$VER-aw1000.tgz"
