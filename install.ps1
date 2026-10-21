$link = "https://github.com/OMetaVR/Distra-Installer/releases/latest/download/DistraInstallerCli.exe"

$outfile = "$env:TEMP\DistraInstallerCli.exe"

Write-Output "Downloading installer to $outfile"

Invoke-WebRequest -Uri "$link" -OutFile "$outfile"

Write-Output ""

Start-Process -Wait -NoNewWindow -FilePath "$outfile"

# Cleanup
Remove-Item -Force "$outfile"
