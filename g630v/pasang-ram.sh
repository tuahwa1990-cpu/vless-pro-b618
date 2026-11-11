#!/bin/sh
# VLESS Pro untuk G630V (AlphaWRT, ARMv7 tanpa VFP, flash kecil).
# Fail besar (vpro, xray) disimpan di RAM (/tmp) dan dimuat turun semula setiap kali but.
# Tetapan, nod dan lesen disimpan di flash: /etc/vlesspro-etc (kecil).
# Guna:  sh pasang-ram.sh pasang   (PELANGGAN: pasang penuh + auto-mula selepas but)
#        sh pasang-ram.sh buang    (buang auto-mula dan pulihkan internet; tetapan dan lesen kekal)
#        sh pasang-ram.sh buang semua (buang semuanya termasuk tetapan, nod dan lesen)
#        sh pasang-ram.sh          (pasang tanpa auto-mula)
#        sh pasang-ram.sh boot     (dipanggil semasa but, tunggu internet)
#        sh pasang-ram.sh hook     (pasang skrip auto-mula sahaja, tanpa muat turun fail besar)
B=${VP_BASE:-https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main}
R=/tmp/vpdata
H=/data/vlesspro
get() { # URL FAIL  (cuba semula + sambung semula jika terhenti)
  i=0
  while [ $i -lt 15 ]; do
    if command -v curl >/dev/null 2>&1; then
      curl -fsSL -C - --connect-timeout 15 --speed-time 20 --speed-limit 2000 -o "$2" "$1" && return 0
    else
      wget -c -T 20 -qO "$2" "$1" 2>/dev/null && return 0
    fi
    i=$((i+1)); sleep 3
  done
  return 1
}
do_hook() {
  cp "$0" /etc/vlesspro-boot.sh && chmod 755 /etc/vlesspro-boot.sh || { echo "GAGAL menyalin skrip"; return 1; }
  [ -f /etc/rc.local ] || { printf '#!/bin/sh\nexit 0\n' > /etc/rc.local; chmod 755 /etc/rc.local; }
  if grep -q vlesspro-g630v /etc/rc.local; then echo "rc.local sudah ada"; else
    awk '/^exit 0/&&!d{print "# vlesspro-g630v"; print "sh /etc/vlesspro-boot.sh boot >/tmp/vlesspro-boot.log 2>&1 &"; d=1} {print} END{if(!d){print "# vlesspro-g630v"; print "sh /etc/vlesspro-boot.sh boot >/tmp/vlesspro-boot.log 2>&1 &"}}' /etc/rc.local > /tmp/rc.tmp && cp /tmp/rc.tmp /etc/rc.local && chmod 755 /etc/rc.local || { echo "GAGAL mengubah rc.local"; return 1; }
  fi
  echo "OK: auto-mula dipasang (skrip: /etc/vlesspro-boot.sh)"
}
if [ "$1" = hook ]; then do_hook; exit $?; fi
if [ "$1" = buang ]; then
  [ -f $H/vp ] && sh $H/vp to-xlite >/dev/null 2>&1
  [ -f $H/run/ui.pid ] && kill $(cat $H/run/ui.pid) 2>/dev/null
  nft delete table ip vpad4 2>/dev/null; nft delete table ip6 vpad6 2>/dev/null; nft delete table inet vpquota 2>/dev/null; nft delete table inet vlesspro 2>/dev/null
  if [ -f /etc/rc.local ]; then
    awk '!/vlesspro-g630v/ && !/vlesspro-boot.sh/' /etc/rc.local > /tmp/rc.tmp && cp /tmp/rc.tmp /etc/rc.local && chmod 755 /etc/rc.local
  fi
  rm -f /etc/vlesspro-boot.sh
  p=$(ps | awk '/vlesspro\/bin\/(vpro|xray)/ && !/awk/{print $1}'); [ -n "$p" ] && kill $p 2>/dev/null
  [ -L /data ] && rm -f /data
  rm -rf /tmp/vpdata
  [ "$2" = semua ] && rm -rf /etc/vlesspro-etc && echo "Tetapan, nod dan lesen dibuang."
  echo "OK: VLESS Pro dibuang. Internet kembali biasa."
  exit 0
fi
if [ "$1" = boot ]; then
  n=0; while [ $n -lt 60 ]; do rm -f /tmp/.vp-probe; get "$B/g630v/vp" /tmp/.vp-probe 2>/dev/null && break; n=$((n+1)); sleep 5; done
  rm -f /tmp/.vp-probe
fi
mkdir -p "$R/vlesspro/bin" /etc/vlesspro-etc
[ -e /data ] && [ ! -L /data ] && { echo "GAGAL: /data wujud dan bukan pautan"; exit 1; }
ln -sfn "$R" /data
ln -sfn /etc/vlesspro-etc "$H/etc"
T=$(mktemp -d /tmp/vpg.XXXXXX); trap 'rm -rf "$T"' EXIT
echo "Memuat turun fail kecil ..."
for f in g630v/vp openwrt/install.sh upgrade.sh; do
  get "$B/$f" "$T/$(basename $f)" || { echo "GAGAL muat turun $f"; exit 1; }
done
echo "Memuat turun vpro ..."
get "$B/g630v/vpro.gz" "$T/vpro.gz" || { echo "GAGAL muat turun vpro"; exit 1; }
echo "Memuat turun xray (±13 MB) ..."
get "$B/g630v/xray.gz" "$T/xray.gz" || { echo "GAGAL muat turun xray"; exit 1; }
[ -f "$H/run/ui.pid" ] && kill $(cat "$H/run/ui.pid") 2>/dev/null
gunzip -c "$T/vpro.gz" > "$H/bin/vpro.new" && gunzip -c "$T/xray.gz" > "$H/bin/xray.new" || { echo "GAGAL membuka fail"; exit 1; }
chmod 755 "$H/bin/vpro.new" "$H/bin/xray.new"
mv -f "$H/bin/vpro.new" "$H/bin/vpro" && mv -f "$H/bin/xray.new" "$H/bin/xray" || { echo "GAGAL menggantikan fail"; exit 1; }
cp "$T/vp" "$T/install.sh" "$T/upgrade.sh" "$H/"
chmod 755 "$H/vp" "$H/install.sh" "$H/upgrade.sh" "$H/bin/vpro" "$H/bin/xray"
# Kemas kini dalam UI tidak sesuai untuk binari tanpa VFP: halakan ke fail yang tiada.
[ -s "$H/etc/update_url" ] || echo "$B/g630v-latest.txt" > "$H/etc/update_url"
if [ "$1" = boot ]; then
  [ "$(cat $H/etc/mode 2>/dev/null)" = vless ] && "$H/vp" boot
  "$H/vp" ui-start
  exit 0
fi
sh "$H/install.sh"
if [ "$1" = pasang ]; then
  do_hook || echo "AMARAN: auto-mula tidak dipasang. Ulang: sh $0 hook"
  echo "SIAP. Selepas reboot router, tunggu 3-5 minit untuk VLESS Pro hidup semula."
else
  echo "Pasang auto-mula selepas but: sh $0 hook"
fi
