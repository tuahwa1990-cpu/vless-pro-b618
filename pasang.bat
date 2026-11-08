@echo off
rem Pemasang VLESS Pro untuk Huawei B618 (Windows 10/11). Klik dua kali atau jalankan: pasang.bat [IP-modem]
setlocal
set IP=%1
if "%IP%"=="" set IP=192.168.8.1
set O=-o StrictHostKeyChecking=accept-new -o ConnectTimeout=8
cd /d "%~dp0"
where ssh >nul 2>&1 || (echo ssh tiada. Pasang "OpenSSH Client" dalam Windows Settings ^> Apps ^> Optional features. & pause & exit /b 1)
echo [1/3] Menyemak modem %IP% ...
ssh %O% root@%IP% "[ -f /online/xlite/bin/xray ]" || (echo AMARAN: XLITE tidak dijumpai atau SSH tidak boleh disambung. & pause & exit /b 1)
ssh %O% root@%IP% "[ -d /online/vlesspro/etc ]"
if %errorlevel%==0 goto naik
echo [2/3] Menghantar fail ...
scp -O %O% -r vlesspro root@%IP%:/online/ || (echo GAGAL menghantar fail & pause & exit /b 1)
echo [3/3] Memasang ...
ssh %O% root@%IP% "sh /online/vlesspro/install.sh"
goto siap
:naik
echo Pemasangan sedia ada dijumpai: naik taraf (tetapan dan lesen dikekalkan).
scp -O %O% -r new upgrade.sh root@%IP%:/online/vlesspro/ || (echo GAGAL menghantar fail & pause & exit /b 1)
ssh %O% root@%IP% "sh /online/vlesspro/upgrade.sh"
:siap
echo.
echo Buka di pelayar: http://%IP%:8990
echo Hantar ID Peranti (di atas) kepada penjual untuk kunci lesen.
pause
