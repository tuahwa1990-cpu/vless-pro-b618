#!/bin/sh
# Keluarkan versi baharu dalam satu arahan.
#
#   RELEASE_SEED=~/keys/vpro-release.seed RELEASE_PUB=BASE64 sh tools/release/rilis.sh [versi|patch]
#
#   sh tools/release/rilis.sh          -> naikkan nombor terakhir (1.1.9 jadi 1.1.10)
#   sh tools/release/rilis.sh 1.2.0    -> guna versi yang diberi
#
# Langkah: semak keadaan repo, naikkan versi, uji dan bina (make-release.sh), bina exe,
# tunjuk ringkasan, minta pengesahan, kemudian commit dan push ke main.
# Tanpa pengesahan, tiada apa dihantar ke GitHub (hanya fail dalam dist/ dan exe).
set -eu

ROOT=$(cd "$(dirname "$0")/../.." && pwd)
cd "$ROOT"

: "${RELEASE_SEED:?Tetapkan RELEASE_SEED ke fail seed penjual}"
: "${RELEASE_PUB:?Tetapkan RELEASE_PUB ke kunci awam (base64)}"

BR=$(git branch --show-current)
[ "$BR" = "main" ] || { echo "GAGAL: anda di cawangan '$BR'. Pindah ke main dahulu (git checkout main)."; exit 1; }
[ -z "$(git status --porcelain)" ] || { echo "GAGAL: ada perubahan belum di-commit. Commit atau batalkan dahulu:"; git status --short; exit 1; }

CUR=$(sed -n 's/^const version = "\(.*\)"/\1/p' src/main.go)
ARG="${1:-patch}"
if [ "$ARG" = "patch" ]; then
  NEW="${CUR%.*}.$(( ${CUR##*.} + 1 ))"
else
  NEW="$ARG"
fi
echo "$NEW" | grep -Eq '^[0-9]+(\.[0-9]+){1,3}$' || { echo "GAGAL: versi '$NEW' tidak sah (contoh 1.1.10)"; exit 1; }
[ "$NEW" != "$CUR" ] && [ "$(printf '%s\n%s\n' "$CUR" "$NEW" | sort -V | tail -1)" = "$NEW" ] \
  || { echo "GAGAL: versi baharu ($NEW) mesti lebih tinggi daripada semasa ($CUR)."; exit 1; }

echo "Versi: $CUR -> $NEW"
sed -i "s/^const version = \"$CUR\"/const version = \"$NEW\"/" src/main.go

sh tools/release/make-release.sh
sh win-setup/build.sh
mkdir -p "dist/$NEW" && mv vless-pro-setup.exe "dist/$NEW/"

echo
echo "================ RINGKASAN ================"
echo "Versi baharu : $NEW (sebelum ini $CUR)"
echo "Fail pelanggan dalam dist/$NEW/ :"
ls -1 "dist/$NEW" | sed 's/^/  /'
echo "Akan di-commit dan di-push ke main:"
git status --short | sed 's/^/  /'
echo "Selepas push, pelanggan yang tekan Kemas kini dalam UI akan dapat $NEW."
echo "==========================================="
if [ "${RILIS_YES:-}" = "ya" ]; then
  A=PUSH
else
  printf 'Uji dahulu pada modem anda. Taip PUSH untuk hantar ke GitHub, atau Enter untuk batal: '
  read A
fi
if [ "$A" != "PUSH" ]; then
  echo "Dibatalkan. Tiada apa dihantar. Fail ada dalam dist/$NEW/."
  echo "Untuk batalkan perubahan tempatan: git checkout -- . "
  exit 0
fi

git add -A
git commit -q -m "Keluaran $NEW"
git push origin main
echo "Siap. $NEW dihantar ke GitHub."
