#!/bin/sh
# Semakan keserasian VLESS Pro. Hanya MEMBACA; tiada apa diubah. Tiada data peribadi dicetak.
has(){ command -v "$1" >/dev/null 2>&1 && echo ada || echo TIADA; }
echo "== VLESS Pro probe =="
m=; [ -r /proc/device-tree/model ] && read m < /proc/device-tree/model 2>/dev/null
[ -z "$m" ] && m=$(grep -m1 -i -E 'machine|model name|Hardware' /proc/cpuinfo 2>/dev/null)
echo "Model     : ${m:-?}"
a=$(uname -m 2>/dev/null); [ -z "$a" ] && a=$(grep -m1 -i -E 'architecture|cpu model|model name' /proc/cpuinfo 2>/dev/null)
echo "Arch      : ${a:-?}"
read kv < /proc/version 2>/dev/null; echo "Kernel    : $(echo "$kv" | sed 's/^\(.\{60\}\).*/\1/')"
[ -r /etc/openwrt_release ] && grep -E 'DISTRIB_(ID|RELEASE)' /etc/openwrt_release
mt=$(grep -m1 MemTotal /proc/meminfo); mf=$(grep -m1 MemFree /proc/meminfo)
echo "RAM       : $mt | $mf"
echo "-- Ruang storan (cari laluan kekal yang boleh ditulis) --"
for p in /online /overlay /opt /etc /usr /data /mnt /tmp; do
  [ -d $p ] || continue
  w=tidak; t=$p/.vp_probe_$$; ( : > $t ) 2>/dev/null && { w=BOLEH; rm -f $t; }
  d=$(df $p 2>/dev/null | grep -v Filesystem | sed -n 1p)
  echo "$p  tulis=$w  | $d"
done 2>/dev/null
echo "-- Alat --"
o=; for c in sh awk sed grep nohup setsid ps kill wget curl tar unzip iptables ip ss netstat opkg; do o="$o $c:$(has $c)"; done; echo "$o"
echo "-- iptables / kernel --"
for t in nat mangle; do
  if iptables -t $t -L -n >/dev/null 2>&1; then echo "iptables $t: BERFUNGSI"; else echo "iptables $t: gagal/tiada"; fi
done
if iptables -t nat -L -n 2>/dev/null | grep -q REDIRECT; then echo "peraturan REDIRECT: sedang digunakan"; fi
[ -e /dev/net/tun ] && echo "/dev/net/tun: ada" || echo "/dev/net/tun: tiada"
echo "-- Proses mengawal (init) --"
[ -d /etc/init.d ] && echo "init.d: ada" ; [ -d /etc/rc.d ] && echo "rc.d: ada"
[ -d /etc/rc.local ] || [ -f /etc/rc.local ] && echo "rc.local: ada"
echo "-- Perisian sedia ada --"
for x in xray sing-box v2ray xlite-core; do f=$(ls /online/*/bin/$x /usr/bin/$x /usr/sbin/$x /opt/bin/$x 2>/dev/null | sed -n 1p); echo "$x: ${f:-tiada}"; done
echo "-- Port --"
for p in 22 80 5000 8990; do if (ss -ltn 2>/dev/null || netstat -ltn 2>/dev/null) | grep -q ":$p "; then echo "port $p: DIGUNAKAN"; else echo "port $p: bebas/tidak diketahui"; fi; done
echo "-- Cadangan binaan --"
case "$a" in
  *armv7*|*armv6*|*ARMv7*|*arm*) echo "Binaan: ARM32 v7 (sedia ada)";;
  *aarch64*|*arm64*) echo "Binaan: ARM64 (perlu binaan baharu)";;
  *mips*) echo "Binaan: MIPS (perlu binaan baharu, RAM kecil berisiko)";;
  *x86_64*|*amd64*) echo "Binaan: x86_64 (perlu binaan baharu)";;
  *) echo "Binaan: tidak diketahui, hantar keputusan ini kepada saya";;
esac
echo "== Tamat. Hantar tangkapan skrin ini. =="
