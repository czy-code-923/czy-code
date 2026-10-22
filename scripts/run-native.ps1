param([ValidateSet('control','agent')][string]$Role='control')
$ErrorActionPreference='Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
$EnvFile = if ($Role -eq 'agent') {'.agent.env'} else {'.env'}
foreach ($Line in Get-Content -LiteralPath $EnvFile) {
    if ($Line -match '^([A-Z_][A-Z0-9_]*)=(.*)$') {
        [Environment]::SetEnvironmentVariable($Matches[1], $Matches[2], 'Process')
    }
}
$Binary=Join-Path (Get-Location) "bin/harbor-$Role.exe"
& $Binary
if ($LASTEXITCODE -ne 0) { throw "$Role exited with $LASTEXITCODE" }
