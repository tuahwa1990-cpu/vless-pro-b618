#!/bin/sh
# Pasang VLESS Pro pada AW1000 Ultra dari telefon (Termux). Guna:
#   curl -fsSL https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main/aw1000/pasang.sh | sh
# Alamat lain: ... | sh -s 192.168.2.1   (jika IP modem sudah ditukar)
# Tidak menyentuh XLITE atau tetapan, lesen dan nod sedia ada.
set -e
IP=${1:-192.168.1.1}
B=https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main
T=$(mktemp -d); trap 'ssh -O exit -o ControlPath="$T/cm" root@$IP >/dev/null 2>&1 || true; rm -rf "$T"' EXIT
echo "Memuat turun pemasang ..."
curl -fsSL "$B/aw1000/install.tgz" -o "$T/i.tgz"
curl -fsSL "$B/aw1000-sha256.txt" -o "$T/s.txt"
W=$(awk '$2=="install.tgz"{print $1}' "$T/s.txt")
G=$(sha256sum "$T/i.tgz" | awk '{print $1}')
[ -n "$W" ] && [ "$W" = "$G" ] || { echo "GAGAL: semakan fail tidak sepadan"; exit 1; }
S="ssh -o StrictHostKeyChecking=accept-new -o ControlMaster=auto -o ControlPath=$T/cm -o ControlPersist=120 root@$IP"
echo "Menghubungi $IP (masukkan kata laluan router jika diminta) ..."
$S '[ -f /data/vlesspro/run/ui.pid ] && kill $(cat /data/vlesspro/run/ui.pid) 2>/dev/null; rm -f /data/vlesspro/bin/vpro; mkdir -p /data && cd /data && tar xzf -' < "$T/i.tgz"
$S 'sh /data/vlesspro/install.sh'
