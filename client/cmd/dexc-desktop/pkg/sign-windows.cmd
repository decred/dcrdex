::
:: This script signs the provided file with the Certificate.pfx file.
::
:: Usage:
:: sign-windows.cmd <file>

if "%1"=="" (
    echo Usage: sign-windows.cmd ^<file^>
    exit /b 1
)

set scriptDir=%~dp0
set certFile=%scriptDir%\..\certs\Certificate.pfx
:: FIXME: obtain the password from command line
set certPassword=YourPassword

:: TODO: obtain the SDK version from the system
set sdkVersion=10.0.20348.0
set signTool="C:\Program Files (x86)\Windows Kits\10\bin\%sdkVersion%\x86\signtool.exe"

if not exist %certFile% (
    echo Certificate %certFile% file not found, not signing.
    exit /b 1
)

%signTool% sign /f %certFile% /t http://timestamp.digicert.com /v /fd sha256  /p %certPassword% %1