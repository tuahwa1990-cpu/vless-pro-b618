#!/bin/sh
# VLESS Pro untuk G630V (AlphaWRT, ARMv7 tanpa VFP, flash kecil).
# Fail besar (vpro, xray) disimpan di RAM (/tmp) dan dimuat turun semula setiap kali but.
# Tetapan, nod dan lesen disimpan di flash: /etc/vlesspro-etc (kecil).
# Guna:  sh pasang-ram.sh          (kali pertama)
#        sh pasang-ram.sh boot     (dipanggil semasa but, tunggu internet)
#        sh pasang-ram.sh hook     (pasang skrip auto-mula sahaja, tanpa muat turun fail besar)
B=${VP_BASE:-https://raw.githubusercontent.com/tuahwa1990-cpu/vless-pro-b618/main}
R=/tmp/vpdata
H=/data/vlesspro
get() { # URL FAIL
  if command -v curl >/dev/null 2>&1; then curl -fsSL "$1" -o "$2" && return 0; fi
  wget -qO "$2" "$1" 2>/dev/null && return 0
  uclient-fetch -qO "$2" "$1" 2>/dev/null
}
if [ "$1" = hook ]; then
  cp "$0" /etc/vlesspro-boot.sh && chmod 755 /etc/vlesspro-boot.sh || { echo "GAGAL menyalin skrip"; exit 1; }
  [ -f /etc/rc.local ] || { printf '#!/bin/sh\nexit 0\n' > /etc/rc.local; chmod 755 /etc/rc.local; }
  if grep -q vlesspro-g630v /etc/rc.local; then echo "rc.local sudah ada"; else
    awk '/^exit 0/&&!d{print "# vlesspro-g630v"; print "sh /etc/vlesspro-boot.sh boot >/tmp/vlesspro-boot.log 2>&1 &"; d=1} {print} END{if(!d){print "# vlesspro-g630v"; print "sh /etc/vlesspro-boot.sh boot >/tmp/vlesspro-boot.log 2>&1 &"}}' /etc/rc.local > /tmp/rc.tmp && cp /tmp/rc.tmp /etc/rc.local && chmod 755 /etc/rc.local || { echo "GAGAL mengubah rc.local"; exit 1; }
  fi
  echo "OK: auto-mula dipasang (skrip: /etc/vlesspro-boot.sh)"; exit 0
fi
if [ "$1" = boot ]; then
  n=0; while [ $n -lt 60 ]; do get "$B/g630v/vpro.gz" /tmp/.vp-probe 2>/dev/null && break; n=$((n+1)); sleep 5; done
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
gunzip -c "$T/vpro.gz" > "$H/bin/vpro" && gunzip -c "$T/xray.gz" > "$H/bin/xray" || { echo "GAGAL membuka fail"; exit 1; }
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
echo "Pasang auto-mula selepas but: sh $0 hook"
