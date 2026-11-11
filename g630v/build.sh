#!/bin/sh
# Bina vpro KHAS G630V (ARMv5 tanpa VFP). Kod bersama di src/ TIDAK diubah:
# salinan sementara src/ ditampal dengan g630v/g630v.patch + g630v/overlay/ sebelum dibina.
# Guna (dari akar repo):  sh g630v/build.sh      -> g630v/vpro.gz, g630v-sha256.txt
set -eu
ROOT=$(cd "$(dirname "$0")/.." && pwd); cd "$ROOT"
PUB="${VP_PUBKEY:-RWQEaRIHBlats0krniS/twQsiXX+YrMtCgz4NYkS7Ia+Bwm4YotZYHwk}"
W=$(mktemp -d); trap 'rm -rf "$W"' EXIT
cp -r src "$W/src"
(cd "$W" && patch -p1 < "$ROOT/g630v/g630v.patch") >/dev/null
cp g630v/overlay/*.go "$W/src/"
(cd "$W/src" && go test . >/dev/null && CGO_ENABLED=0 GOOS=linux GOARCH=arm GOARM=5 \
  go build -trimpath -ldflags "-s -w -X main.pubkey=$PUB" -o "$W/vpro" .)
gzip -9nc "$W/vpro" > g630v/vpro.gz
(cd g630v && printf '%s  vp\n%s  vpro.gz\n' "$(sha256sum vp|cut -d' ' -f1)" "$(sha256sum vpro.gz|cut -d' ' -f1)") > g630v-sha256.txt
echo "OK: g630v/vpro.gz dan g630v-sha256.txt dijana"
