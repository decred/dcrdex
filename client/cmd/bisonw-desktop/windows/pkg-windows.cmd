:: This script builds the bisonw-desktop MSI installer.

@echo off

call windows\env-windows.cmd

:: Run build
call windows\build-windows.cmd %*

:: Check the error level after the first command
if %errorlevel% equ 0 (
    echo Build completed successfully.
    echo Building MSI 
    dotnet build --property:Platform=x64 --configuration Release --output build\msi -noWarn:WIX1076 windows\windows-msi\BisonWallet_Installer.wixproj
    echo MSI built in build\msi
) else (
    echo Error occurred during build.
)

echo Signing %exeFile%
call windows\sign-windows.cmd %exeFile%