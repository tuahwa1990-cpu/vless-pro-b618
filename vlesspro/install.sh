#!/bin/sh
# Pemasang VLESS Manager Pro (B618). Tidak menyentuh XLITE, mod, atau /system.
H=/online/vlesspro
[ -d $H ] || { echo "Folder $H tiada. Hantar folder vlesspro ke /online dahulu."; exit 1; }
mount -o remount,rw /online 2>/dev/null
chmod 755 $H/vp $H/install.sh $H/bin/vpro 2>/dev/null
[ -f $H/bin/xray ] && chmod 755 $H/bin/xray
mkdir -p $H/etc $H/run $H/backup
[ -f $H/etc/mode ] || echo xlite > $H/etc/mode
if [ ! -s $H/etc/pass ]; then
  u=; read u < /proc/sys/kernel/random/uuid
  u=${u%%-*}
  [ -n "$u" ] || u=$(date +%s)
  echo "$u" > $H/etc/pass; chmod 600 $H/etc/pass
  : > $H/etc/pass_initial
  echo "Kata laluan UI baharu: $u"
else
  echo "Kata laluan UI sedia ada dikekalkan."
fi
$H/vp doctor
echo "ID Peranti: $($H/bin/vpro id)"
L=$($H/bin/vpro lic | awk -F= '$1=="state"{print $2}')
if [ "$L" = "active" ]; then
  echo "Lesen sedia ada dikesan dan sah. Tiada tindakan diperlukan."
else
  echo "Belum ada lesen aktif (status: $L). Tampal kunci lesen di UI: Tetapan > Lesen."
fi
$H/vp ui-start
sleep 2
echo "Buka di telefon: http://192.168.8.1:8990  (pengguna: apa-apa, kata laluan di atas)"
echo "XLITE tidak diubah. Mod semasa: $($H/vp status | awk -F= '$1=="mod"{print $2}')"
