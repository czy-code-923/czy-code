"""Explicit acceptance check on a manager: transient test host and child Agent only.

Run as root with --manager-acceptance. Reads the local control.env without printing
credentials. Does not replace an installed Agent, change firewall or start proxies.
"""
import argparse, hashlib, io, json, os, re, subprocess, tempfile, time
import urllib.request, urllib.error, zipfile
from pathlib import Path

def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--manager-acceptance', action='store_true', required=True)
    parser.parse_args()
    assert os.geteuid() == 0, 'root required to read the private manager environment'
    settings = dict(line.split('=', 1) for line in Path('/etc/harbor/control.env').read_text().splitlines() if '=' in line and not line.startswith('#'))
    def admin(path, method='GET', body=None):
        req = urllib.request.Request('http://127.0.0.1:8080/api/v1/'+path, method=method,
            data=None if body is None else json.dumps(body).encode(),
            headers={'Authorization':'Bearer '+settings['ADMIN_TOKEN'], 'Content-Type':'application/json'})
        with urllib.request.urlopen(req, timeout=15) as response: return json.load(response)
    host = admin('hosts', 'POST', {'name':'acceptance-enrollment-'+str(int(time.time()))})['id']
    process = None
    try:
        enrollment = admin('hosts/'+host+'/enrollment', 'POST', {})
        origin, ticket = re.search(r"sh \"\$f\" '(https://[^']+)' '([a-f0-9]{64})'", enrollment['command']).groups()
        def fetch(path, body=None):
            req = urllib.request.Request(origin+path, data=None if body is None else json.dumps(body).encode(), headers={'Content-Type':'application/json'})
            with urllib.request.urlopen(req, timeout=120) as response: return response.read()
        manifest = json.loads(fetch('/api/v1/agent/releases/linux-amd64'))
        archive = fetch('/api/v1/agent/releases/linux-amd64?download=1')
        assert len(archive) == manifest['size'] and hashlib.sha256(archive).hexdigest() == manifest['sha256']
        with zipfile.ZipFile(io.BytesIO(archive)) as package:
            assert set(package.namelist()) == {'harbor-agent','sing-box','frps','frpc','xray'}
            binary = package.read('harbor-agent')
        print('PASS: public HTTPS package download, size, SHA-256 and fixed contents', flush=True)
        registration = json.loads(fetch('/api/v1/agent/enroll', {'ticket':ticket}))
        assert registration['host_id'] == host
        try: fetch('/api/v1/agent/enroll', {'ticket':ticket})
        except urllib.error.HTTPError as error: assert error.code == 401
        else: raise AssertionError('Used ticket accepted twice')
        print('PASS: one-time enrollment and replay rejection', flush=True)
        with tempfile.TemporaryDirectory(prefix='harbor-enrollment-check-') as temporary:
            folder = Path(temporary); folder.chmod(0o755)
            agent = folder/'harbor-agent'; agent.write_bytes(binary); agent.chmod(0o755)
            environment = {**os.environ, 'CONTROL_URL':origin, 'HOST_ID':host, 'AGENT_TOKEN':registration['agent_token'], 'AGENT_MODE':'simulation', 'ENABLE_LIVE':'0', 'GOMEMLIMIT':'64MiB', 'GOMAXPROCS':'1'}
            process = subprocess.Popen(['runuser','-u','harbor','--',str(agent)], env=environment, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
            for _ in range(45):
                assert process.poll() is None, 'Acceptance Agent stopped unexpectedly'
                if admin('hosts/'+host+'/enrollment')['state'] == 'connected': break
                time.sleep(1)
            else: raise AssertionError('No real Agent heartbeat received')
            row = next(row for row in admin('hosts') if row['id'] == host)
            assert row['agent_version'] == '0.7.1' and row['platform'] == 'linux/amd64' and row['reported_addresses']
            print('PASS: unprivileged real Linux Agent, verified HTTPS heartbeat, version/platform/IP report', flush=True)
            process.terminate(); process.wait(timeout=15); process=None
            installer = folder/'install.py'; installer.write_bytes(fetch('/api/v1/agent/install.py'))
            result = subprocess.run(['python3',str(installer),origin,'a'*64], capture_output=True, text=True)
            assert result.returncode != 0 and 'Existing Agent found' in result.stderr
            print('PASS: existing manager Agent overwrite protection', flush=True)
    finally:
        if process is not None:
            process.terminate(); process.wait(timeout=15)
        admin('hosts/'+host, 'DELETE')
        print('Acceptance child stopped and its test host removed; installed services preserved', flush=True)
    print('HARBOR_ENROLLMENT_ACCEPTANCE_PASSED')

if __name__ == '__main__': main()
