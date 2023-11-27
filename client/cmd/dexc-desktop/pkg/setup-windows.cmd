:: This script bootstraps the build environment for Windows.  It installs git and
:: PowerShell, clones the dcrdex repo and runs the setup-windows-stage2.ps1 script 
:: which will install the toolchain and libraries required for the build.
::
:: Running the script:
:: setup-windows.cmd <repoBranch> <repoUrl> 
::
:: Both parameters are optional.  If not provided, https://github.com/decred/dcrdex@master
:: will be cloned.

@echo off

set GitVersion=2.43.0
set PowerShellVersion=7.4.0

if "%1" neq "" (
    set repoBranch=%1
) else (
    set repoBranch=master
)

if "%2" neq "" (
    set repoUrl=%2
) else (
    set repoUrl=https://github.com/decred/dcrdex
)

set GitInstaller=Git-%GitVersion%-64-bit.exe

@echo Installing git... 
curl -sLO https://github.com/git-for-windows/git/releases/download/v%GitVersion%.windows.1/%GitInstaller%

:: Create a silent install file
(
  echo [Setup]
  echo Lang=default
  echo Dir=C:\Program Files\Git
  echo Group=Git
  echo NoIcons=0
  echo SetupType=default
  echo Components=ext,ext\shellhere,ext\guihere,gitlfs,assoc,assoc_sh,scalar
  echo Tasks=
  echo EditorOption=VIM
  echo CustomEditorPath=
  echo DefaultBranchOption= 
  echo PathOption=Cmd
  echo SSHOption=OpenSSH
  echo TortoiseOption=false
  echo CURLOption=OpenSSL
  echo CRLFOption=CRLFCommitAsIs
  echo BashTerminalOption=MinTTY
  echo GitPullBehaviorOption=Rebase
  echo UseCredentialManager=Enabled
  echo PerformanceTweaksFSCache=Enabled
  echo EnableSymlinks=Disabled
  echo EnablePseudoConsoleSupport=Disabled
  echo EnableFSMonitor=Disabled
) > git_install_options.ini

start /wait %GitInstaller% /VERYSILENT /NORESTART /NOCANCEL /LOADINF=git_install_options.ini

:: Install PowerShell
@echo Installing PowerShell...
curl -OLs  https://github.com/PowerShell/PowerShell/releases/download/v%PowerShellVersion%/PowerShell-%PowerShellVersion%-win-x64.msi
PowerShell-%PowerShellVersion%-win-x64.msi /quiet /norestart

:: Clone the dcrdex repository
if exist dcrdex rd /s /q dcrdex
@echo Cloning %repoUrl%@%repoBranch%...
"c:\Program Files\Git\bin\git.exe" clone -b %repoBranch% %repoUrl%
cd dcrdex\client\cmd\dexc-desktop

:: Install toolchain and SDKs
powershell  -ExecutionPolicy Bypass -File pkg\setup-windows-stage2.ps1
