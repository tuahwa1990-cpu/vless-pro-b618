#!/bin/sh
# Pemasang VLESS Manager Pro (OpenWrt / AW1000 Ultra). Tidak menyentuh XLITE atau firmware.
H=/data/vlesspro
[ -d $H ] || { echo "Folder $H tiada."; exit 1; }
chmod 755 $H/vp $H/install.sh $H/bin/vpro 2>/dev/null
[ -f $H/bin/xray ] && chmod 755 $H/bin/xray
mkdir -p $H/etc $H/run $H/backup
[ -f $H/etc/mode ] || echo xlite > $H/etc/mode
if [ ! -s $H/etc/pass ]; then
  echo "admin" > $H/etc/pass; chmod 600 $H/etc/pass
  echo "Kata laluan UI awal: admin (boleh ditukar di Tetapan)"
else
  echo "Kata laluan UI sedia ada dikekalkan."
fi
$H/vp doctor
echo "ID Peranti: $(VP_HOME=$H $H/bin/vpro id)"
L=$(VP_HOME=$H $H/bin/vpro lic | awk -F= '$1=="state"{print $2}')
[ "$L" = "active" ] && echo "Lesen sedia ada sah." || echo "Belum ada lesen aktif (status: $L). Tampal kunci di UI: Tetapan > Lesen."
if [ ! -s $H/etc/listen ]; then
  A=$(ifconfig br-lan 2>/dev/null | awk '/inet addr:/{sub(/addr:/,"",$2);print $2;exit}')
  [ -n "$A" ] && echo "$A:8990" > $H/etc/listen
fi
LS=$(cat $H/etc/listen 2>/dev/null); LS=${LS:-192.168.2.1:8990}
$H/vp ui-start
sleep 2
echo "Buka di telefon: http://$LS  (kata laluan awal: admin)"
echo "Mod semasa: $($H/vp status | awk -F= '$1=="mod"{print $2}')"
echo "Pasang auto-mula selepas but: $H/vp hook-install (pilihan)"
