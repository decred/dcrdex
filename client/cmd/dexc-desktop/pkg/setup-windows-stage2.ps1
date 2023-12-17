# PowerShell script that installs the toolchain and required SDKs for building 
# dexc-desktop and the MSI installer on Windows.
#
# This script is normally called from setup-windows.cmd

$GoVersion = "1.21.5"
$NodeVersion = "21.4.0"
$MSysVersion = "2023-10-26"
$VSBuildToolsVersion = "17"

# Set error action preference to Stop
$ErrorActionPreference = "Stop"

$workDir = $PWD.Path

# Initialize temp directory where the installers will be downloaded
$tmpDir = "$env:TEMP\vs_setup"
if (Test-Path -Path $tmpDir -PathType Container) {
  Remove-Item -Path $tmpDir -Recurse -Force
}
New-Item -ItemType Directory -Path $tmpDir
Set-Location -Path $tmpDir

# Install Go
$GoInstaller = "go$GoVersion.windows-amd64.msi"
Write-Host "Installing go..." -ForegroundColor Cyan
Invoke-WebRequest -Uri https://go.dev/dl/$GoInstaller -OutFile $GoInstaller
Start-Process msiexec.exe -ArgumentList "/i $GoInstaller /quiet" -Wait

#  Install msys2
$_msysV = $MSysVersion -replace '-'
$MSys2Installer = "msys2-x86_64-$_msysV.exe"
$MSysInstallPath = "C:\msys64"

Write-Host "Installing MSYS2..." -ForegroundColor Cyan
Invoke-WebRequest -Uri https://github.com/msys2/msys2-installer/releases/download/$MSysVersion/$MSys2Installer -OutFile $MSys2Installer
Start-Process $MSys2Installer -ArgumentList "install --root $MSysInstallPath --confirm-command" -Wait
[Environment]::SetEnvironmentVariable('Path', "$([Environment]::GetEnvironmentVariable('Path', 'Machine'));$MSysInstallPath\ucrt64\bin", 'Machine')

# Have to hardcode the path to bash.exe, there's no way to put this in a variable
function msys() { C:\msys64\usr\bin\bash.exe @('-lc') + @Args; }
Write-Host "Installing mingw toolchain..." -ForegroundColor Cyan
msys ' '
msys 'pacman --noconfirm -Syuu'
msys 'pacman --noconfirm -S mingw-w64-ucrt-x86_64-gcc'

# Install Visual Studio Build Tools
Write-Host "Installing VS Build Tools..." -ForegroundColor Cyan
$VsBtInstaller = "vs_BuildTools.exe"
Invoke-WebRequest -Uri https://aka.ms/vs/$VSBuildToolsVersion/release/vs_BuildTools.exe -OutFile $VsBtInstaller
# emit vsconfig file that defines the required workloads and SDKs to facilitate a silent install
@"
{
  "version": "1.0",
  "components": [
    "Microsoft.VisualStudio.Workload.ManagedDesktopBuildTools",
    "Microsoft.VisualStudio.Component.Roslyn.Compiler",
    "Microsoft.Component.MSBuild",
    "Microsoft.VisualStudio.Component.CoreBuildTools",
    "Microsoft.VisualStudio.Workload.MSBuildTools",
    "Microsoft.VisualStudio.Component.Windows10SDK",
    "Microsoft.VisualStudio.Component.VC.CoreBuildTools",
    "Microsoft.VisualStudio.Component.VC.Tools.x86.x64",
    "Microsoft.VisualStudio.Component.VC.Redist.14.Latest",
    "Microsoft.VisualStudio.Component.VC.CMake.Project",
    "Microsoft.VisualStudio.Component.TestTools.BuildTools",
    "Microsoft.Net.Component.4.8.SDK",
    "Microsoft.Net.Component.4.7.2.TargetingPack",
    "Microsoft.VisualStudio.Component.VC.ASAN",
    "Microsoft.VisualStudio.Component.TextTemplating",
    "Microsoft.VisualStudio.Component.VC.CoreIde",
    "Microsoft.VisualStudio.ComponentGroup.NativeDesktop.Core",
    "Microsoft.VisualStudio.Component.Windows10SDK.20348",
    "Microsoft.VisualStudio.Workload.VCTools",
    "Microsoft.VisualStudio.Component.NuGet.BuildTools",
    "Microsoft.Net.ComponentGroup.DevelopmentPrerequisites",
    "Microsoft.VisualStudio.Component.TypeScript.TSServer",
    "Microsoft.Net.Component.4.8.TargetingPack",
    "Microsoft.Net.ComponentGroup.4.8.DeveloperTools",
    "Microsoft.NetCore.Component.Runtime.8.0",
    "Microsoft.NetCore.Component.SDK",
    "Microsoft.Component.ClickOnce.MSBuild",
    "Microsoft.VisualStudio.Wcf.BuildTools.ComponentGroup"
  ]
}
"@ | Set-Content -Path "vsbuildtools.vsconfig"
# Run the VS Build Tools installer
Start-Process $VsBtInstaller -ArgumentList "--wait --config $tmpDir\vsbuildtools.vsconfig --passive --norestart" -Wait
# opt out of dotnet telemetry
[Environment]::SetEnvironmentVariable('DOTNET_CLI_TELEMETRY_OPTOUT', "1", 'Machine')

# Install node.js
$NodeInstaller = "node-v$NodeVersion-x86.msi"
Write-Host "Installing Node.js..." -ForegroundColor Cyan
Invoke-WebRequest -Uri https://nodejs.org/dist/v21.4.0/node-v$NodeVersion-x86.msi -OutFile $NodeInstaller
Start-Process msiexec.exe -ArgumentList "/i $NodeInstaller /quiet" -Wait

# Allow PowerShell to be run by unprivileged users
Set-ExecutionPolicy -ExecutionPolicy Bypass -Scope MachinePolicy -Force

# Change back to the repo clone
Set-Location -Path $workDir

$msg = "Installation complete.  
Open a new command prompt to apply environment variables and run 
pkg\build-windows.cmd or
pkg\pkg-windows.cmd
"

Write-Host $msg -ForegroundColor Cyan
