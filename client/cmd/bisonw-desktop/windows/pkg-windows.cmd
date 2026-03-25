:: This script builds the bisonw-desktop MSI installer.

@echo off

call windows\env-windows.cmd

:: Parse flags.
set "XMR_ENABLED=0"
set "SKIP_BUILD=0"
:parse_args
if "%~1"=="" goto done_args
if "%~1"=="--xmr" set "XMR_ENABLED=1"
if "%~1"=="--skip-build" set "SKIP_BUILD=1"
shift
goto parse_args
:done_args

if "%SKIP_BUILD%"=="1" (
    echo Skipping build, packaging existing files in build\windows...
    goto :MSI
)

:: Run build. Only forward --xmr if set; build-windows.cmd does not
:: understand --skip-build.
if "%XMR_ENABLED%"=="1" (
    call windows\build-windows.cmd --xmr
) else (
    call windows\build-windows.cmd
)

if %errorlevel% neq 0 (
    echo Error occurred during build.
    exit /b 1
)
echo Build completed successfully.

:MSI
echo Building MSI
dotnet build --property:Platform=x64 --configuration Release --output build\msi -noWarn:WIX1076 -p:XmrEnabled=%XMR_ENABLED% -p:CrossCompiled=%SKIP_BUILD% windows\windows-msi\BisonWallet_Installer.wixproj
echo MSI built in build\msi

echo Signing %exeFile%
call windows\sign-windows.cmd %exeFile%
