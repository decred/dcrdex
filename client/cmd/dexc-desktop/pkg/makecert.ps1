# This script creates a self signed certificate to sign the DCRDEX executable
# and installer, suitable for testing.

# If the default PowerShell Execution Policy is set to Restricted, run this using:
# powershell -ExecutionPolicy Bypass -File pkg\makecert.ps1

# Set variables
$certSubject = "CN=DCRDEXCodeSigningCertificate"
$certPassword = "YourPassword"
$certFriendlyName = "DCRDEX Code Signing Cert"
$certPath = "certs"
$certExportPath = "$certPath\Certificate.pfx"

if (-not (Test-Path -Path $certPath -PathType Container)) {
  New-Item -ItemType Directory -Path $certPath -Force
}

# Create a self-signed certificate
$cert = New-SelfSignedCertificate -DnsName $certSubject -CertStoreLocation Cert:\CurrentUser\My -KeyUsage DigitalSignature  -KeySpec Signature -KeyAlgorithm RSA -KeyLength 2048 -Type CodeSigningCert -Subject $certSubject

# Export the certificate with private key to a PFX file
Export-PfxCertificate -Cert Cert:\CurrentUser\My\$($cert.Thumbprint) -FilePath $certExportPath -Password (ConvertTo-SecureString -String $certPassword -Force -AsPlainText)

Write-Host "Self-signed code signing certificate created and exported to $certExportPath"



