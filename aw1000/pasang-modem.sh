#!/bin/sh
# Pasang VLESS Pro terus DI DALAM modem AW1000 Ultra (ConnectBot / Termius / SSH).
# Tampal pada terminal modem (selepas log masuk root):
#   wget -qO /tmp/pm.sh https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main/aw1000/pasang-modem.sh || curl -fsSL -o /tmp/pm.sh https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main/aw1000/pasang-modem.sh; sh /tmp/pm.sh
# Tidak menyentuh XLITE atau tetapan, lesen dan nod sedia ada. Fail disemak sha256 sebelum dipasang.
set -e
B=${VP_BASE:-https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main}
D=${VP_DEST:-/data}
T=$(mktemp -d /tmp/vpm.XXXXXX); trap 'rm -rf "$T"' EXIT
get() { # get URL FAIL
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2" && return 0; fi
  if command -v wget >/dev/null 2>&1; then wget -qO "$2" "$1" && return 0; fi
  if command -v uclient-fetch >/dev/null 2>&1; then uclient-fetch -qO "$2" "$1" && return 0; fi
  return 1
}
echo "Memuat turun pemasang ..."
get "$B/aw1000/install.tgz" "$T/i.tgz" || { echo "GAGAL: tidak dapat muat turun (semak internet modem; HTTPS mungkin tidak disokong oleh wget di modem)"; exit 1; }
get "$B/aw1000-sha256.txt" "$T/s.txt" || { echo "GAGAL: tidak dapat muat turun fail semakan"; exit 1; }
W=$(awk '$2=="install.tgz"{print $1}' "$T/s.txt")
G=$(sha256sum "$T/i.tgz" | awk '{print $1}')
[ -n "$W" ] && [ "$W" = "$G" ] || { echo "GAGAL: semakan fail tidak sepadan"; exit 1; }
echo "Semakan fail OK. Memasang ..."
[ -f "$D/vlesspro/run/ui.pid" ] && kill $(cat "$D/vlesspro/run/ui.pid") 2>/dev/null || true
rm -f "$D/vlesspro/bin/vpro"
mkdir -p "$D" && cd "$D" && tar xzf "$T/i.tgz"
# Xray baharu (>= 26.10) di /usr/bin menolak nod VLESS tanpa TLS beralamat IP: guna salinan sendiri v26.3.27
if [ ! -x "$D/vlesspro/bin/xray" ] && [ ! -x /opt/xlite/bin/xray ]; then
  echo "Xray tidak dijumpai: memuat turun ..."
  if command -v unzip >/dev/null 2>&1 && get "https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-arm64-v8a.zip" "$T/x.zip"; then
    unzip -oq "$T/x.zip" xray -d "$T" && mkdir -p "$D/vlesspro/bin" && cp "$T/xray" "$D/vlesspro/bin/xray" && chmod 755 "$D/vlesspro/bin/xray" && echo "Xray dipasang." || echo "AMARAN: gagal memasang Xray."
  else
    if [ -x /usr/bin/xray ]; then echo "AMARAN: tidak dapat muat turun Xray lama; guna /usr/bin/xray (nod tanpa TLS beralamat IP mungkin ditolak)."; else echo "AMARAN: tidak dapat memuat turun Xray di modem. Gunakan pemasang telefon (aw1000/pasang.sh) atau .exe."; fi
  fi
fi
[ -n "$VP_NOINSTALL" ] && { echo "Ujian: langkau install.sh"; exit 0; }
sh "$D/vlesspro/install.sh"
