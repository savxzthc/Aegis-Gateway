@echo off
setlocal
pushd "%~dp0frontend"
if not exist node_modules (
  call npm install --prefer-offline
  if errorlevel 1 exit /b 1
)
call npm run build
if errorlevel 1 exit /b 1
popd
go build -o aegis-gateway.exe .
if errorlevel 1 exit /b 1
if /I "%~1"=="--check" exit /b 0
.\aegis-gateway.exe
