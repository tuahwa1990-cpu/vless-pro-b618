@echo off
rem Pemasang VLESS Pro untuk Huawei B618 (Windows 10/11). Klik dua kali atau jalankan: pasang.bat [IP-modem]
setlocal
set IP=%1
if "%IP%"=="" set IP=192.168.8.1
set O=-o StrictHostKeyChecking=accept-new -o ConnectTimeout=8
cd /d "%~dp0"
where ssh >nul 2>&1 || (echo ssh tiada. Pasang "OpenSSH Client" dalam Windows Settings ^> Apps ^> Optional features. & pause & exit /b 1)
echo [1/4] Menyemak modem %IP% ...
ssh %O% root@%IP% "echo ok" >nul 2>&1 || (echo GAGAL: tidak dapat sambung SSH ke %IP%. Pastikan PC disambung ke WiFi modem dan SSH modem dihidupkan. & pause & exit /b 1)
set NOX=0
ssh %O% root@%IP% "[ -f /online/xlite/bin/xray ] || [ -f /online/vlesspro/bin/xray ]"
if not %errorlevel%==0 set NOX=1
if %NOX%==1 echo XLITE tidak dijumpai: Xray akan dimuat turun dan dipasang bersama VLESS Pro.
ssh %O% root@%IP% "[ -d /online/vlesspro/etc ]"
if %errorlevel%==0 goto naik
echo [2/4] Menghantar fail ...
scp -O %O% -r vlesspro root@%IP%:/online/ || (echo GAGAL menghantar fail & pause & exit /b 1)
call :getx || exit /b 1
echo [3/4] Memasang ...
ssh %O% root@%IP% "sh /online/vlesspro/install.sh"
goto siap
:naik
echo [2/4] Pemasangan sedia ada dijumpai: naik taraf (tetapan dan lesen dikekalkan).
scp -O %O% -r new upgrade.sh root@%IP%:/online/vlesspro/ || (echo GAGAL menghantar fail & pause & exit /b 1)
call :getx || exit /b 1
echo [3/4] Memasang ...
ssh %O% root@%IP% "sh /online/vlesspro/upgrade.sh"
:siap
echo [4/4] Siap.
echo.
echo Buka di pelayar: http://%IP%:8990
echo Hantar ID Peranti (di atas) kepada penjual untuk kunci lesen.
pause
exit /b 0

:getx
if not %NOX%==1 exit /b 0
where curl >nul 2>&1 || (echo curl tiada. Guna Windows 10/11 terkini. & pause & exit /b 1)
where tar >nul 2>&1 || (echo tar tiada. Guna Windows 10/11 terkini. & pause & exit /b 1)
set T=%TEMP%\vpx
rd /s /q "%T%" >nul 2>&1
mkdir "%T%"
echo Memuat turun Xray (sekitar 15 MB) ...
curl -fL -o "%T%\x.zip" https://github.com/XTLS/Xray-core/releases/latest/download/Xray-linux-arm32-v7a.zip || (echo GAGAL memuat turun Xray & pause & exit /b 1)
tar -xf "%T%\x.zip" -C "%T%" xray || (echo GAGAL membuka zip Xray & pause & exit /b 1)
ssh %O% root@%IP% "mkdir -p /online/vlesspro/bin"
scp -O %O% "%T%\xray" root@%IP%:/online/vlesspro/bin/xray || (echo GAGAL menghantar Xray. Ruang storan modem mencukupi? & pause & exit /b 1)
rd /s /q "%T%" >nul 2>&1
exit /b 0
