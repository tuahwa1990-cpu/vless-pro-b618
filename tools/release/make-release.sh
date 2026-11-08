#!/bin/sh
# Jana fail keluaran VLESS Pro (ARMv7: Huawei B618 dan Gteniq MiFi).
#
# Guna (dari akar repo):  sh tools/release/make-release.sh
# Hasil dalam dist/<versi>/:
#   vlesspro-<versi>-armv7.tgz  -> pakej yang sama dengan payload vless-pro-setup.exe
#   b618-sha256.txt             -> untuk dikemas kini ke repo (kemas kini dalam UI)
#   CAPTION.txt                 -> teks untuk siaran ke pelanggan
#   Tidak push apa-apa. Push ke GitHub dibuat secara manual selepas semakan.
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

# Kunci awam mesti sepadan dengan kunci persendirian penjual (lihat tools/issue-licence).
PUB="${VP_PUBKEY:-RWRp62b6XpBvZps8xQpP/ZNrpruxOM6u/pVMLFACu1srkHmaEXtTofO7}"

VER=$(sed -n 's/^const version = "\(.*\)"/\1/p' src/main.go)
[ -n "$VER" ] || { echo "GAGAL: versi tidak dijumpai dalam src/main.go"; exit 1; }

# upd.go memasukkan upgrade.sh melalui go:embed, jadi salinan di src/ mesti sama dengan akar.
cp upgrade.sh src/upgrade.sh

echo "[1/5] Ujian unit ..."
(cd src && go test ./...) || { echo "GAGAL: ujian tidak lulus"; exit 1; }

B=$(mktemp -d)
trap 'rm -rf "$B"' EXIT

echo "[2/5] Bina vpro ARMv7 ($VER) ..."
(cd src && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=7 \
  go build -trimpath -ldflags "-s -w -X main.pubkey=$PUB" -o "$B/vpro" .)

echo "[3/5] Susun pakej (struktur sama seperti payload pemasang) ..."
P="$B/pkg"
mkdir -p "$P/vlesspro/bin" "$P/new"
cp vlesspro/install.sh "$P/vlesspro/"
cp vlesspro/vp "$P/vlesspro/"
cp "$B/vpro" "$P/vlesspro/bin/vpro"
cp vlesspro/vp "$P/new/vp"
cp "$B/vpro" "$P/new/vpro"
cp upgrade.sh "$P/"
chmod 755 "$P/vlesspro/vp" "$P/vlesspro/install.sh" "$P/vlesspro/bin/vpro" "$P/new/vp" "$P/new/vpro" "$P/upgrade.sh"

OUT="dist/$VER"
mkdir -p "$OUT"
TGZ="vlesspro-$VER-armv7.tgz"

echo "[4/5] Cipta $TGZ dan SHA256 ..."
tar -C "$P" -czf "$OUT/$TGZ" .
( cd "$P" && sha256sum vlesspro/bin/vpro vlesspro/vp ) | sed 's#vlesspro/bin/vpro#vpro#; s#vlesspro/vp#vp#' > "$OUT/b618-sha256.txt"
echo "$VER" > "$OUT/b618-latest.txt"
( cd "$P" && find . -type f | sort | xargs sha256sum ) > "$OUT/SHA256SUMS"

echo "[5/5] Tulis CAPTION.txt ..."
cat > "$OUT/CAPTION.txt" <<CAP
VLESS Manager Pro $VER (ARMv7)

Peranti yang disokong:
- Huawei B618
- Gteniq MiFi (iQ02X55 / iQ03X55)

Pasang baharu: guna vless-pro-setup.exe
Naik taraf: guna vless-pro-setup.exe (pilih Baiki) atau kemas kini dari dalam UI
CAP

echo
echo "Selesai. Fail dalam $OUT/:"
ls -1 "$OUT"
echo "Semak dahulu sebelum hantar: tar tzf $OUT/$TGZ"
