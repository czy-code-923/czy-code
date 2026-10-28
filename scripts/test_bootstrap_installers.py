"""Bootstrap behavior tests. Package managers and OS changes are mocked, never run."""
import os,shutil,subprocess,tempfile,unittest
from pathlib import Path

ROOT=Path(__file__).resolve().parents[1]
SHELL=shutil.which('sh') if os.name!='nt' else next((str(p) for p in [Path('D:/Software/Git/bin/bash.exe'),Path('C:/Program Files/Git/bin/bash.exe')] if p.exists()),None)
PS=shutil.which('pwsh') or shutil.which('powershell')

class LinuxBootstrap(unittest.TestCase):
    def run_shell(self,body):
        if not SHELL:self.skipTest('POSIX shell unavailable')
        library=(ROOT/'scripts/install-starhub.sh.in').read_text().split('# ENTRYPOINT')[0]
        script=library+'''\nPATH=/nonexistent
uname(){ printf Linux; }
id(){ printf 0; }
python3(){ return 0; }
curl(){ return 0; }
sleep(){ return 0; }
'''+body
        result=subprocess.run([SHELL,'-c',script],capture_output=True,text=True)
        return result
    def test_existing_environment_is_reused(self):
        r=self.run_shell('''have_python(){ return 0; }
install_base(){ exit 41; };install_docker(){ exit 42; }
docker(){ case "$*" in *--format*) printf linux;; esac; }
ensure_environment
''')
        self.assertEqual(r.returncode,0,r.stderr+r.stdout)
    def test_missing_dependencies_install_and_start_engine(self):
        r=self.run_shell('''ready=0;engine=0
have_python(){ [ "$ready" = 1 ]; }
install_base(){ printf BASE_INSTALLED;ready=1; }
install_docker(){ printf DOCKER_INSTALLED;docker(){ if [ "$engine" != 1 ]; then return 1; fi;case "$*" in *--format*) printf linux;;esac; }; }
systemctl(){ printf SERVICE_STARTED;engine=1; }
ensure_environment
''')
        self.assertEqual(r.returncode,0,r.stderr+r.stdout)
        for marker in ('BASE_INSTALLED','DOCKER_INSTALLED','SERVICE_STARTED'):self.assertIn(marker,r.stdout)
    def test_wrong_container_mode_is_rejected(self):
        r=self.run_shell('have_python(){ return 0; };docker(){ case "$*" in *--format*) printf windows;;esac; };ensure_environment')
        self.assertNotEqual(r.returncode,0);self.assertIn('Linux Docker containers',r.stdout)
    def test_unsupported_distribution_fails_before_install(self):
        r=self.run_shell('ID=unknown;install_docker');self.assertNotEqual(r.returncode,0)

class WindowsBootstrap(unittest.TestCase):
    def run_ps(self,body):
        if not PS:self.skipTest('PowerShell unavailable')
        library=(ROOT/'scripts/install-starhub.ps1.in').read_text().split('# ENTRYPOINT')[0]
        with tempfile.TemporaryDirectory(prefix='starhub-bootstrap-test-') as folder:
            path=Path(folder)/'test.ps1';path.write_text(library+'\n$env:OS="Windows_NT"\n'+body,encoding='utf-8-sig')
            return subprocess.run([PS,'-NoProfile','-NonInteractive','-File',str(path)],capture_output=True,text=True)
    def test_existing_environment_is_reused(self):
        r=self.run_ps('''function Refresh-InstallPath {}
function Find-InstallPython {return 'python-fixture.exe'}
function Get-InstallDockerOS {return 'linux'}
function Ensure-InstallWinget {throw 'Existing environment was changed'}
$result=Ensure-InstallEnvironment
if($result -is [array] -or $result -ne 'python-fixture.exe'){throw 'Unexpected Python result'}
''');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
    def test_missing_python_install_output_does_not_corrupt_python_path(self):
        r=self.run_ps('''function Refresh-InstallPath {}
$global:found=$false
function Find-InstallPython {if($global:found){return 'installed-python.exe'};return $null}
function Ensure-InstallWinget {}
function winget {$global:found=$true;$global:LASTEXITCODE=0;Write-Output 'package installation progress'}
function Get-InstallDockerOS {return 'linux'}
$result=Ensure-InstallEnvironment
if($result -is [array] -or $result -ne 'installed-python.exe'){throw 'Installer output leaked into executable path'}
''');self.assertEqual(r.returncode,0,r.stdout+r.stderr)
    def test_windows_container_mode_is_not_silently_changed(self):
        r=self.run_ps('''function Refresh-InstallPath {}
function Find-InstallPython {return 'python.exe'}
function Get-InstallDockerOS {return 'windows'}
try {Ensure-InstallEnvironment;throw 'Expected rejection'}catch {if($_ -notmatch 'Switch it to Linux containers'){throw};Write-Host 'EXPECTED_REJECTION'}
''');self.assertEqual(r.returncode,0,r.stdout+r.stderr);self.assertIn('EXPECTED_REJECTION',r.stdout)

    def test_wsl_restart_stops_before_creating_panel(self):
        r=self.run_ps("""function Refresh-InstallPath {}
function Find-InstallPython {return 'python.exe'}
function Get-InstallDockerOS {return ''}
function Test-InstallAdministrator {return $true}
function Get-CimInstance {return [pscustomobject]@{ProductType=1}}
function Get-WindowsOptionalFeature {return [pscustomobject]@{State='Disabled'}}
function Enable-WindowsOptionalFeature {return [pscustomobject]@{RestartNeeded=$true}}
function Ensure-InstallWinget {}
function winget {$global:LASTEXITCODE=0;Write-Output 'Installed prerequisite'}
function Test-Path {return $false}
$env:ProgramFiles=[IO.Path]::GetTempPath();$env:LOCALAPPDATA=[IO.Path]::GetTempPath()
Ensure-InstallEnvironment
Write-Host 'PANEL-WAS-CREATED'
""")
        self.assertIn(r.returncode,(3010,194),r.stdout+r.stderr)
        self.assertIn('Restart Windows',r.stdout)
        self.assertNotIn('PANEL-WAS-CREATED',r.stdout)

if __name__=='__main__':unittest.main()
