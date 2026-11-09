#!/bin/sh
# Pemasang VLESS Pro untuk Huawei B618 (jalankan dari Termux).
# Guna dari folder:  sh pasang.sh [IP-modem]      (lalai 192.168.8.1)
# Atau terus dari GitHub (tanpa folder):
#   curl -fsSL https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main/pasang.sh | sh
IP=${1:-192.168.8.1}
D=$(cd "$(dirname "$0")" && pwd)
if [ ! -f "$D/vlesspro/install.sh" ] || [ ! -d "$D/new" ]; then
  command -v curl >/dev/null || { echo "curl tiada. Jalankan dahulu:  pkg install curl"; exit 1; }
  B=https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main
  D=$(mktemp -d); trap 'rm -rf "$D"' EXIT
  mkdir -p "$D/vlesspro/bin" "$D/new"
  echo "Memuat turun pemasang dari GitHub ..."
  for f in vlesspro/install.sh vlesspro/vp vlesspro/bin/vpro new/vp new/vpro upgrade.sh b618-sha256.txt; do
    curl -fsSL "$B/$f" -o "$D/$f" || { echo "GAGAL memuat turun $f (semak internet telefon)"; exit 1; }
  done
  for p in vpro vp; do
    W=$(awk -v n=$p '$2==n{print $1}' "$D/b618-sha256.txt")
    for g in "$D/new/$p"; do G=$(sha256sum "$g" | awk '{print $1}'); [ -n "$W" ] && [ "$W" = "$G" ] || { echo "GAGAL: semakan fail $p tidak sepadan"; exit 1; }; done
  done
  cp "$D/new/vp" "$D/vlesspro/vp"; cp "$D/new/vpro" "$D/vlesspro/bin/vpro"
  chmod 755 "$D/vlesspro/vp" "$D/vlesspro/install.sh" "$D/vlesspro/bin/vpro" "$D/new/vp" "$D/new/vpro" "$D/upgrade.sh"
fi
SSH="ssh -n -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8 root@$IP"
SCP="scp -O -o StrictHostKeyChecking=accept-new -o ConnectTimeout=8"
command -v ssh >/dev/null && command -v scp >/dev/null || { echo "ssh/scp tiada. Jalankan dahulu:  pkg install openssh"; exit 1; }
echo "[1/4] Menyemak modem $IP ..."
X=$($SSH '[ -f /online/xlite/bin/xray ] && echo ada; [ -d /online/vlesspro/etc ] && echo lama; [ -f /online/vlesspro/bin/xray ] && echo vx; true' 2>/dev/null) || { echo "GAGAL: tidak dapat sambung SSH ke $IP. Pastikan telefon disambung ke WiFi modem dan SSH modem dihidupkan."; exit 1; }
NOX=0
case "$X" in *ada*|*vx*) ;; *) NOX=1; echo "XLITE tidak dijumpai: Xray akan dimuat turun dan dipasang bersama VLESS Pro.";; esac
getx() {
  [ "$NOX" = 1 ] || return 0
  command -v curl >/dev/null && command -v unzip >/dev/null || { echo "Jalankan dahulu:  pkg install curl unzip"; exit 1; }
  T=$(mktemp -d)
  echo "Memuat turun Xray v26.3.27 (sekitar 15 MB) ..."
  curl -fL -o "$T/x.zip" https://github.com/XTLS/Xray-core/releases/download/v26.3.27/Xray-linux-arm32-v7a.zip || { echo "GAGAL memuat turun Xray (semak internet telefon)"; exit 1; }
  (cd "$T" && unzip -oq x.zip xray) || { echo "GAGAL membuka zip Xray"; exit 1; }
  $SSH 'mkdir -p /online/vlesspro/bin'
  $SCP "$T/xray" root@$IP:/online/vlesspro/bin/xray || { echo "GAGAL menghantar Xray (ruang storan modem mencukupi?)"; exit 1; }
  rm -rf "$T"
}
case "$X" in
 *lama*)
  echo "[2/4] Pemasangan sedia ada dijumpai: naik taraf (tetapan dan lesen dikekalkan)."
  $SCP -r "$D/new" "$D/upgrade.sh" root@$IP:/online/vlesspro/ || { echo "GAGAL menghantar fail"; exit 1; }
  getx
  echo "[3/4] Memasang ..."
  $SSH 'sh /online/vlesspro/upgrade.sh';;
 *)
  echo "[2/4] Menghantar fail ..."
  $SCP -r "$D/vlesspro" root@$IP:/online/ || { echo "GAGAL menghantar fail"; exit 1; }
  getx
  echo "[3/4] Memasang ..."
  $SSH 'sh /online/vlesspro/install.sh';;
esac
echo "[4/4] Siap."
echo "Buka di Chrome: http://$IP:8990"
echo "Hantar ID Peranti (di atas) kepada penjual untuk mendapatkan kunci lesen."
case "$X" in *ada*) echo "XLITE tidak diubah dan boleh ditukar semula bila-bila masa.";; esac
