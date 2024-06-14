@echo off

:: This build script has been tested with Windows 10 and Windows 11.
:: 
:: See Build-Windows.md for more information.

call pkg\env-windows.cmd

if not exist "%libDir%" (
    mkdir %libDir%
)

if exist "%outputDir%" (
    rd /s /q %outputDir%
)
mkdir %outputDir%

set sitePath=..\..\webserver\site
set workDir=%cd%

:: If the site bundle does not exist, build it.
if not exist "%sitePath%\dist" (
    echo Building site bundle...
    cd %sitePath%
    call npm clean-install
    call npm run build
    cd %workDir%
)

echo Fetching and extracting the latest WebView2 SDK...
curl -sSL "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2" | tar -xf - -C "%libDir%"

if %ERRORLEVEL% NEQ 0 (
    echo Fetching and extracting WebView2 failed! Do you have tar and curl?
    exit /b 1
)

echo Building bisonw-desktop with CGO configured...
set CGO_CXXFLAGS="-I%libDir%\build\native\include"
set CGO_ENABLED=1
go build -v -ldflags="-H windowsgui" -o %exeFile%

if %ERRORLEVEL% NEQ 0 goto ERROR
echo Build completed
copy /Y %libDir%\build\native\x64\WebView2Loader.dll %outputDir%



echo The WebView2Loader.dll file should be included with bisonw-desktop.exe
exit /b 0

:ERROR
echo Build FAILED.
exit /b 1
