# Install a limited user task at logon. This does not install a privileged Windows service.
$ErrorActionPreference='Stop'
$ProjectRoot=Split-Path -Parent $PSScriptRoot
$Runner=Join-Path $PSScriptRoot 'run-native.ps1'
if ($Runner -match '["\r\n]') {throw 'Unsupported characters in installation path'}
if (-not (Test-Path -LiteralPath (Join-Path $ProjectRoot 'bin/harbor-agent.exe'))) {throw 'Build Agent first'}
if (-not (Test-Path -LiteralPath (Join-Path $ProjectRoot '.agent.env'))) {throw 'Create .agent.env first; see INSTALL.md'}
if (Get-ScheduledTask -TaskName 'HarborAgent' -ErrorAction SilentlyContinue) {throw 'Existing task found. Review it before updating.'}
$Action=New-ScheduledTaskAction -Execute 'powershell.exe' -Argument "-NoProfile -File `"$Runner`" -Role agent" -WorkingDirectory $ProjectRoot
$Trigger=New-ScheduledTaskTrigger -AtLogOn -User ([System.Security.Principal.WindowsIdentity]::GetCurrent().Name)
$Principal=New-ScheduledTaskPrincipal -UserId ([System.Security.Principal.WindowsIdentity]::GetCurrent().Name) -LogonType Interactive -RunLevel Limited
$Settings=New-ScheduledTaskSettingsSet -RestartCount 3 -RestartInterval (New-TimeSpan -Minutes 1) -ExecutionTimeLimit ([TimeSpan]::Zero) -MultipleInstances IgnoreNew -AllowStartIfOnBatteries -DontStopIfGoingOnBatteries
Register-ScheduledTask -TaskName 'HarborAgent' -Action $Action -Trigger $Trigger -Principal $Principal -Settings $Settings | Out-Null
Start-ScheduledTask -TaskName 'HarborAgent'
Write-Host 'Installed current-user logon task HarborAgent. Keep the private project path fixed. Task requires user logon.'
