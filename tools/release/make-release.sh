#!/bin/sh
# Jana fail keluaran VLESS Pro (ARMv7: Huawei B618).
#
# Keperluan: RELEASE_SEED (fail seed penjual) dan RELEASE_PUB (kunci awam, base64).
#   Jana kunci sekali sahaja:  (cd tools/release-sign && go run . -gen ~/keys/vpro-release.seed)
#
# Guna (dari akar repo):
#   RELEASE_SEED=~/keys/vpro-release.seed RELEASE_PUB=BASE64 sh tools/release/make-release.sh
#
# Hasil dalam dist/<versi>/:
#   vlesspro-<versi>-armv7.tgz  -> pakej: fail pemasang + SHA256SUMS + SHA256SUMS.sig
#   b618-sha256.txt, b618-latest.txt -> untuk repo (kemas kini dari GitHub)
#   CAPTION.txt                 -> teks siaran ke pelanggan
# Fail dalam repo (vlesspro/, new/, upgrade.sh, src/upgrade.sh) juga disegerakkan.
# Tidak push apa-apa.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

: "${RELEASE_SEED:?Tetapkan RELEASE_SEED ke fail seed penjual}"
: "${RELEASE_PUB:?Tetapkan RELEASE_PUB ke kunci awam (base64)}"
# Kunci awam lesen (sama seperti sebelum ini)
PUB="${VP_PUBKEY:-RWQEaRIHBlats0krniS/twQsiXX+YrMtCgz4NYkS7Ia+Bwm4YotZYHwk}"

VER=$(sed -n 's/^const version = "\(.*\)"/\1/p' src/main.go)
[ -n "$VER" ] || { echo "GAGAL: versi tidak dijumpai dalam src/main.go"; exit 1; }

B=$(mktemp -d)
trap 'rm -rf "$B"' EXIT

# upd.go memasukkan upgrade.sh melalui go:embed, jadi salinan di src/ mesti sama dengan akar.
cp upgrade.sh src/upgrade.sh

echo "[1/6] Ujian unit ..."
(cd src && go test ./...) || { echo "GAGAL: ujian tidak lulus"; exit 1; }

echo "[2/6] Bina vpro ARMv7 ($VER) ..."
(cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath -ldflags "-s -w -X main.pubkey=$PUB -X main.relpub=$RELEASE_PUB" -o "$B/vpro" .)

echo "[3/6] Segerakkan repo dan susun pakej ..."
cp "$B/vpro" vlesspro/bin/vpro
cp "$B/vpro" new/vpro
gzip -9nc "$B/vpro" > new/vpro.gz
cp vlesspro/vp new/vp
P="$B/pkg"
mkdir -p "$P/vlesspro/bin" "$P/new"
cp vlesspro/install.sh vlesspro/vp "$P/vlesspro/"
cp "$B/vpro" "$P/vlesspro/bin/vpro"
cp vlesspro/vp "$P/new/vp"
cp "$B/vpro" "$P/new/vpro"
cp upgrade.sh "$P/"
chmod 755 "$P/vlesspro/vp" "$P/vlesspro/install.sh" "$P/vlesspro/bin/vpro" "$P/new/vp" "$P/new/vpro" "$P/upgrade.sh"
echo "$VER" > "$P/VERSION"

echo "[4/6] Tandatangan SHA256SUMS ..."
( cd "$P" && find . -type f | sed 's#^\./##' | sort | xargs sha256sum ) > "$B/SHA256SUMS"
cp "$B/SHA256SUMS" "$P/SHA256SUMS"
(cd tools/release-sign && go build -o "$B/relsign" .)
"$B/relsign" -seed "$RELEASE_SEED" -in "$P/SHA256SUMS" -out "$P/SHA256SUMS.sig"

OUT="dist/$VER"
mkdir -p "$OUT"
TGZ="vlesspro-$VER-armv7.tgz"

echo "[5/6] Cipta $TGZ dan fail repo ..."
tar -C "$P" -czf "$OUT/$TGZ" .
# versi kecil (hanya fail kemas kini, tandatangan sama) untuk Pasang dari fail
tar -C "$P" --exclude=./vlesspro -czf "$OUT/vlesspro-$VER-armv7-kemaskini.tgz" .
( cd "$P" && sha256sum vlesspro/bin/vpro vlesspro/vp ) | sed 's#vlesspro/bin/vpro#vpro#; s#vlesspro/vp#vp#' > "$OUT/b618-sha256.txt"
echo "$VER" > "$OUT/b618-latest.txt"
cp "$OUT/b618-sha256.txt" b618-sha256.txt
cp "$OUT/b618-latest.txt" b618-latest.txt
# Tandatangan senarai semak dalam talian dan nota keluaran (halaman Kemas kini)
"$B/relsign" -seed "$RELEASE_SEED" -in b618-sha256.txt -out b618-sha256.txt.sig
sh tools/release/notes.sh "$VER" new/vpro.gz > b618-notes.json

echo "[6/6] Tulis CAPTION.txt ..."
cat > "$OUT/CAPTION.txt" <<CAP
VLESS Manager Pro $VER (ARMv7)

Peranti yang disokong:
- Huawei B618

Kemas kini: buka UI > Kemas kini > Pasang dari fail, pilih fail .tgz ini.
Pemasangan baharu: lihat panduan pemasangan.
CAP

echo
echo "Selesai. Fail dalam $OUT/:"
ls -1 "$OUT"
