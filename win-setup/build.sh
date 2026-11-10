#!/bin/sh
# Bina vless-pro-setup.exe. Jalankan dari akar repo selepas kemas kini fail dalam vlesspro/, new/ dan upgrade.sh.
set -e
D=$(mktemp -d)
cp main.go go.mod "$D"/ 2>/dev/null || cp win-setup/main.go win-setup/go.mod "$D"/
mkdir -p "$D/payload"
cp -r vlesspro new upgrade.sh "$D/payload/"
rm -f "$D/payload/new/vpro.gz"
cp aw1000/install.tgz "$D/payload/aw1000.tgz"
cd "$D" && CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -ldflags="-s -w" -o "$OLDPWD/vless-pro-setup.exe" .
echo "Dibina: vless-pro-setup.exe"
