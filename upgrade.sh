#!/bin/sh
# Naik taraf VLESS Pro (B618). Tetapan, lesen, nod dan XLITE tidak diubah.
H=/online/vlesspro
N=$H/new
[ -f $N/vpro ] || { echo "Fail baharu tiada di $N"; exit 1; }
mount -o remount,rw /online 2>/dev/null
cp $H/bin/vpro $H/backup/vpro.prev 2>/dev/null
cp $H/vp $H/backup/vp.prev 2>/dev/null
p=; [ -f $H/run/ui.pid ] && read p < $H/run/ui.pid
[ -n "$p" ] && kill $p 2>/dev/null
sleep 1
[ -n "$p" ] && kill -9 $p 2>/dev/null
rm $H/run/ui.pid 2>/dev/null
rm $H/bin/vpro 2>/dev/null
cp $N/vpro $H/bin/vpro || { echo "GAGAL salin vpro"; cp $H/backup/vpro.prev $H/bin/vpro; exit 1; }
cp $N/vp $H/vp
chmod 755 $H/bin/vpro $H/vp
rm $N/vpro $N/vp 2>/dev/null
rmdir $N 2>/dev/null
sh $H/vp ui-start
sleep 2
$H/bin/vpro | awk 'NR==1'
echo "Mod semasa: $(sh $H/vp status | awk -F= '$1=="mod"{print $2}')"
echo "Buka: http://192.168.8.1:8990"
