param([ValidateSet('compose','build')][string]$Mode='compose')
$ErrorActionPreference='Stop'
Set-Location -LiteralPath (Split-Path -Parent $PSScriptRoot)
function Invoke-Checked([string]$Program,[string[]]$Arguments) {
    & $Program @Arguments
    if ($LASTEXITCODE -ne 0) { throw "$Program failed with exit code $LASTEXITCODE" }
}
if ($Mode -eq 'compose') {
    if (-not (Get-Command docker -ErrorAction SilentlyContinue)) { throw 'Install and start Docker Desktop with Linux containers first.' }
    Invoke-Checked docker @('compose','version')
    Invoke-Checked python @('scripts/prepare.py')
    Invoke-Checked docker @('compose','config','--quiet')
    Invoke-Checked docker @('compose','up','--build','--detach','--wait','--wait-timeout','180')
    Write-Host 'Harbor ready: http://127.0.0.1:5173. ADMIN_TOKEN is in .env; keep this file private.'
} else {
    New-Item -ItemType Directory -Force bin | Out-Null
    $env:CGO_ENABLED='0'
    foreach ($Program in @('control','agent','firewall','gateway')) {
        Invoke-Checked go @('build','-trimpath','-ldflags=-s -w','-o',"bin/harbor-$Program.exe","./cmd/$Program")
    }
    Push-Location web
    try { Invoke-Checked npm @('ci'); Invoke-Checked npm @('run','build') } finally { Pop-Location }
    Write-Host 'Built bin/ and web/dist/. See docs/INSTALL.md for PostgreSQL and native setup.'
}
