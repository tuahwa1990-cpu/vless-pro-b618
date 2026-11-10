#!/bin/sh
# Bina vless-pro-setup.exe. Jalankan dari akar repo selepas kemas kini fail dalam vlesspro/, new/ dan upgrade.sh.
set -e
D=$(mktemp -d)
cp main.go go.mod "$D"/ 2>/dev/null || cp win-setup/main.go win-setup/go.mod "$D"/
mkdir -p "$D/payload"
cp -r vlesspro new upgrade.sh "$D/payload/"
rm -f "$D/payload/new/vpro.gz"
for f in "$D/payload/vlesspro/bin/vpro" "$D/payload/new/vpro"; do gzip -9n "$f"; done
cp aw1000/install.tgz "$D/payload/aw1000.tgz"
# Edisi AW3000T sahaja (EDISI=aw3000t): Xray arm64 v26.3.27 terbenam, hasil vless-pro-setup-aw3000t.exe
LF="-s -w"; OUT=vless-pro-setup.exe
if [ "${EDISI:-}" = "aw3000t" ]; then
  XZ=${XRAY_ZIP:-/tmp/xray-arm64.zip}
  [ -s "$XZ" ] || curl -fsSL -o "$XZ" https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-arm64-v8a.zip
  cp "$XZ" "$D/payload/xray-arm64.zip"
  LF="-s -w -X main.edisi=aw3000t"; OUT=vless-pro-setup-aw3000t.exe
fi
cd "$D" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="$LF" -o "$OLDPWD/$OUT" .
echo "Dibina: $OUT"
