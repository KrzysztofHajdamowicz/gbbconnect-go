# Windows Service installation

Run the following commands in an elevated PowerShell session. Copy the release
binary to its permanent location before installing the service because Windows
Service Control Manager records its absolute path.

```powershell
$installDir = Join-Path $env:ProgramFiles "gbbconnect"
$dataDir = Join-Path $env:ProgramData "gbbconnect"

New-Item -ItemType Directory -Force $installDir, $dataDir
Copy-Item .\gbbconnect_windows_amd64.exe `
  (Join-Path $installDir "gbbconnect.exe")
Copy-Item .\gbbconnect.yaml (Join-Path $dataDir "gbbconnect.yaml")

& (Join-Path $installDir "gbbconnect.exe") config validate `
  --config (Join-Path $dataDir "gbbconnect.yaml")
& (Join-Path $installDir "gbbconnect.exe") service install
sc.exe start gbbconnect
sc.exe query gbbconnect
```

The automatically started service uses:

- configuration: `%ProgramData%\gbbconnect\gbbconnect.yaml`;
- state and daily logs: `%ProgramData%\gbbconnect\state`;
- Windows Application Event Log source: `gbbconnect`.

View recent events in Event Viewer or PowerShell:

```powershell
Get-WinEvent -FilterHashtable @{
  LogName = "Application"
  ProviderName = "gbbconnect"
} -MaxEvents 50
```

Service STOP and system SHUTDOWN requests trigger the same graceful shutdown as
SIGTERM on Linux. The service stops accepting cloud requests, disconnects MQTT,
and saves plant state before reporting `Stopped`.

To update the executable or configuration:

```powershell
sc.exe stop gbbconnect
Copy-Item .\gbbconnect_windows_amd64.exe `
  (Join-Path $env:ProgramFiles "gbbconnect\gbbconnect.exe") -Force
& (Join-Path $env:ProgramFiles "gbbconnect\gbbconnect.exe") config validate `
  --config (Join-Path $env:ProgramData "gbbconnect\gbbconnect.yaml")
sc.exe start gbbconnect
```

To uninstall, first stop the service:

```powershell
sc.exe stop gbbconnect
& (Join-Path $env:ProgramFiles "gbbconnect\gbbconnect.exe") service uninstall
```

Uninstalling removes the service registration and Event Log source but
preserves configuration, state, logs, and the executable. The same EXE remains
a normal foreground CLI when launched interactively.
