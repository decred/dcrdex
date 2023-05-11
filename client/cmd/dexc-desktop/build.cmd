@echo off

:: This build script has been tested with Windows 10 and Windows 11.
:: 
:: Before running this script, prepare your environment as follows:
::
::  1. Install Git. https://git-scm.com/download/win
::    a. In the "Adjusting your PATH environment" step, use the option
::       "Git from the command line and also from 3rd-party software".
::    b. In the "Configuring the line ending conversions" step, use the option
::       "Checkout as-is, commit as-is".
::  2. Install Go. https://go.dev/dl/
::  3. (Optional) If you need to build the client/webserver/site bundle,
::     install Node.js. https://nodejs.org/en/download/current
::    a. Install Python from the Windows Store.
::    b. Install Visual Studio Build Tools.
::       https://visualstudio.microsoft.com/thank-you-downloading-visual-studio/?sku=BuildTools
::       (choose C++ workload in installer and pick the latest SDK version)
::  4. Install the Windows SDK, if you skipped Visual Studio Build Tools above.
::     Two options:
::    a. Standalone SDK installer.
::       https://developer.microsoft.com/en-us/windows/downloads/sdk-archive/
::     OR
::    b. With the Visual Studio Build Tools (see link in Node.js step above).
::  5. Reboot.
::  6. Install MSYS2. https://www.msys2.org/
::     Default install folder is C:\msys64.
::    a. If the installer does not automatically open a terminal when done,
::       launch the "UCRT64" terminal from the start menu.
::    b. Update everything with `pacman -Suy`. It will close the terminal when
::       it completes.  Start it again.
::    c. Install the ucrt64 runtime variety of the GCC compilers.
::       `pacman -S mingw-w64-ucrt-x86_64-gcc`
::  7. Edit the *user* `PATH` to include `C:\msys64\ucrt64\bin`. Use the "Edit"
::     button at (System Properties -> Advanced -> Environment Variables...).
::  8. Close all terminals, or logout/in, or reboot.
::  9. Open cmd.exe (the plain windows console, not msys or the git console),
::     and check `gcc --version`.
::  10. Ensure `tar` and `curl` are also available. By default, they should be
::      on the *system* `PATH` for up-to-date Windows installs.
::  11. If not on a dcrdex release branch, either obtain the `dist` folder from
::      another machine and put it under `client/webserver/site` or build it
::      from that folder with `npm ci` and `npm run build`. See the Node.js
::      install instructions above.
::
:: NOTE: On every machine tested, the Webview2 *runtime* was already installed,
:: but if you have removed it or Edge itself, it may be necessary to install:
:: https://developer.microsoft.com/en-us/microsoft-edge/webview2/#download-section 

echo "Fetching and extracting the latest WebView2 SDK..."
mkdir libs\webview2
curl -sSL "https://www.nuget.org/api/v2/package/Microsoft.Web.WebView2" | tar -xf - -C libs\webview2

if %ERRORLEVEL% NEQ 0 (
    echo "Fetching and extracting WebView2 failed! Do you have (bsd)tar and curl?"
    exit /b 1
)

echo "Building dexc-desktop with CGO configured..."
set CGO_CXXFLAGS="-I%cd%\libs\webview2\build\native\include"
set CGO_ENABLED=1
:: If you get an embed error in the build, you may need the site bundle built.
:: See the step above regarding the `dist` folder.
go build -v -ldflags="-H windowsgui"

if %ERRORLEVEL% NEQ 0 goto ERROR
echo "Build complete!"
copy /Y libs\webview2\build\native\x64\WebView2Loader.dll .
echo "The WebView2Loader.dll file should be included with dexc-desktop.exe"
exit /b 0

:ERROR
echo "Build FAILED."
exit /b 1
