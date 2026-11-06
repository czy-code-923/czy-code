# ���� �� VPS ������� 0.25.5

���� Linux һ����װ�� Nginx��Certbot / Cloudflare DNS ������ܿ��������֣����а�װ�ɵ����޸����������ݿ�����ơ�[0.25.5 ����˵��](docs/RELEASE-v0255.md)��

��������Э�顢�û�Ȩ�ޡ������������ͻ��˲����붩�Ĺ��ܡ�����ʹ����ʽ��������У�顢ͣд���ݺ�ʧ�ܻ��ˣ�������������Զ�� Agent �����ֿ�������

������ Android NekoBox / sing-box / Hiddify ������������򼯳�վ���ã�������Ȩ�ޱ����Ķ������ġ��ڵ����ά�� API���ӹ̰�װ���ƣ����ӿ�ѡ SS 2022 / Snell �������ݼ�����֤��༭ɾ�������� HTTPS �����������鿴��[0.23 ��������ݱ߽�](docs/WORKFLOW-v023.md)��

���Ľڵ�� IPv4 / ������ڱ�����̨ԭ����IPv6 ʹ�á�ԭ��-v6�������� `snell_pro` / `snell_pro-v6`����������� ID�������԰��ȶ��˵�����ѡ�񣬸�������ı���Ȩ����������������ͻ��˱������Ƴ�ͻʱ��ȷ��ʾ�޸ģ��������޷���������á�[����˵��](docs/WORKFLOW-v021.md)��

�����̣�**�������� �� �����ں���Э�� �� �û� / �û���Ȩ�� �� �ͻ��˲��� �� �û�ԭ���ķ���**��ʹ�� Clash Verge / FlClash ���豸����ʱѡ�����ģʽ����ģʽͳ�ƶ��ķ��ʣ���֤����ʵ�����豸�����������Զ������û�����Ȩ�������ؽ��û���

## һ���������ܹ�����

�ű���� Python >=3.9��HTTPS ���ع����� Docker Linux �������棬ȱ��ʱ�Զ���װ�����п��û���ֱ�Ӹ��á�Ԥ��������ܹ�ѡ�񲢺���̶� SHA-256������Ҫ Go / Node�����а�װ�����ݾ��������˿ڳ�ͻ��ֹͣ�������ô��������������塣

Linux��Debian 11+ / Ubuntu 22.04+��Fedora��Rocky / Alma / RHEL 8+ / CentOS Stream��openSUSE / SLES �� systemd ϵͳ���� root �� sudo����

```bash
sh -c 'set -eu; S=; if [ "$(id -u)" != 0 ]; then command -v sudo >/dev/null || { echo "Run as root"; exit 1; }; S=sudo; fi; if ! command -v curl >/dev/null; then if command -v apt-get >/dev/null; then $S apt-get update; $S apt-get install -y curl ca-certificates; elif command -v dnf >/dev/null; then $S dnf install -y curl ca-certificates; elif command -v zypper >/dev/null; then $S zypper --non-interactive install curl ca-certificates; elif command -v apk >/dev/null; then $S apk add --no-cache curl ca-certificates; else echo "Install curl first on this OS"; exit 1; fi; fi; F=$(mktemp); curl --proto "=https" --tlsv1.2 -fL https://github.com/czy-code-923/czy-code/releases/download/v0.25.5-distribution/install-starhub.sh -o "$F"; $S sh "$F" --public; rm -f "$F"'
```

Windows 10/11 **����Ա PowerShell**���Զ�Ѱ�һ�װ Python��WinGet��Docker Desktop / WSL ������������ȴ� Linux ���档�״����� WSL ��Ҫ����ʱ����ʾ������������ͬһ��������Զ��������л����� Windows ���������� Docker �״����� / ���⻯Ҫ��Windows Server ʹ�� Linux �������

```powershell
Invoke-WebRequest https://github.com/czy-code-923/czy-code/releases/download/v0.25.5-distribution/install-starhub.ps1 -OutFile "$env:TEMP/install-starhub.ps1"; & "$env:TEMP/install-starhub.ps1" -Public
```

Ĭ�Ϲ��� IP:18080 ����֤��ҳ��HTTP������װ��������ð������� HTTPS���ڲ� API�����������������ݿ���ֻ�����ػ�����Ҫ���������ʣ�ȥ�� `--public` / `-Public`��Windows Ĭ�ϵ�ǰ�û���¼����Linux ������ systemd ������Alpine / OpenRC ���Զ�׼��������������ð��� `quickstart.py --foreground` ����Լ��ļල����������װ����ж�ؾ� Docker����������/���ݻ�ſ����ݿ�˿ڡ�

[����Դ������ƽ̨����](https://github.com/czy-code-923/czy-code/releases/tag/v0.25.5-distribution) �� [�ֶ� / Compose / ���߰�װ](docs/INSTALL.md)

## ��̨�汾���Զ�����

���롸ϵͳ���� �� ��̨�汾������� GitHub ������ʽ�棬ȷ�Ϻ��ύ���¡�֧�ֵ� Linux systemd ��װ�ɶ����������ִ������ر���ҳ����ȡ�����񡣸��°�У��ʧ�ܱ���ԭ�汾��Ӧ�ú󽡿����ʧ����ָ����ݿ���ļ��������ڼ������ܶ����жϣ����ݱ����ڽ� root �ɶ��� `/var/lib/starhub-updater/`��Windows��Compose ������δ����ʱҳ����ȷ��ʾ�ֶ�������ֻ����������Ա������ί�� API ��Կ�޸���Ȩ�ޡ�

�Զ����²��л����� Nginx�������ں˻����� VPS �� Agent������ `/opt/harbor` �ɱ�����������װ���֣�������֧�� Linux ��װ�����÷�ʽ�ͼ���Ҫ��� [����˵��](docs/RELEASE-v0238.md)��

## ��ѯ����ַ����ʱ IP �������

��ѯ���õ�ַ��ԭ��������ַ���������״̬������ʾ��¼���ƣ����°�װĬ�ϸ�Ŀ¼Ϊ `/opt/starhub-control`����ǰ���� VPS Ϊ `/opt/harbor`���ű����Զ�ʶ��

```bash
curl -fL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/panel-access.py -o /tmp/starhub-panel-access.py && sudo python3 /tmp/starhub-panel-access.py status
```

��ǰ���� VPS ��ֱ�Ӳ�ѯ��

```bash
sudo python3 /opt/harbor/panel-access.py status
```

��������ʱ��ʱ���� **IP:18081��30 �����Զ��ر�**�������������������á�HTTP δ���ܣ������������ʽ���ã�ƽʱ�رգ����� API / ���ݿ�˿ڼ���ֻ�ڻػ������������Ʒ���ǽ��ֹ 18081��������д���ҳ�˿ڣ��ű������ȫ�ַ���ǽ��

```bash
sudo python3 /opt/harbor/panel-access.py enable-ip --allow-http --minutes 30
```

�ֶ��رգ�

```bash
sudo python3 /opt/harbor/panel-access.py disable-ip
```

��ȷ��Ҫ�����ɼ� `--persistent`�����Ƽ��ڲ��������糤��ʹ�� HTTP��Ҳ��ͨ�� SSH ����ת�����лػ���ҳ�����迪���κα��ù����˿ڣ�

```bash
ssh -N -L 18080:127.0.0.1:5173 YOUR_USER@YOUR_VPS
# ����������� http://127.0.0.1:18080
```

�°�װ�ѱ��ؽű�·����Ϊ `/opt/starhub-control/panel-access.py`��ת��Ŀ�껻Ϊ��ѯ���ص� `web_listener`��Windows ������ `python "$env:ProgramData/StarhubControl/panel-access.py" status`����ʱ���м� `enable-ip --allow-http --foreground`�����ڱ��뱣������ʱ�� Ctrl+C �رա�Compose ʹ���Լ��Ķ˿�ӳ���� SSH ת��������� [��װ�ĵ�](docs/INSTALL.md)��

## Linux һ��ж��

Ĭ��ֻж�ر���Ŀ����������������ݿ⡢˽�����úͱ��ݡ�ȥ�� `--apply` ����Ԥ��������ɾ�����������б��ݺ���ʽ���� `--purge-data`��Զ�� Agent ��������ҳ��̨���ۡ�

```bash
curl -fL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -o /tmp/starhub-uninstall.py && sudo python3 /tmp/starhub-uninstall.py --apply
```

���ڵ�ǰ���� VPS ��װ�ı���ж�����

```bash
sudo python3 /opt/harbor/uninstall.py --apply
```

## ����һֱ�ȴ�ǩ�� / ��װ�����޸�

IP + �˿��ܷ���ֻ������������������Linux systemd ��װ�ᰲװ Nginx��Certbot��Cloudflare DNS ������ܿ��������֣����б��� / Nginx ������ʶ�𲢱���ԭ��վ���ɰ氲װ��ֻ�п�����͸��·��񣬿�ʹ�� [���ذ�װ���޸�����](docs/GATEWAY.md)��������װ�����ؽ����ơ�

## Linux ��ȡ������Ա��¼����

����ʾ��̨����������е� `ADMIN_TOKEN`���������ɡ��ֻ����������ƣ��뱦���˺��޹ء���������Ƶ������¼ҳ����Ҫճ�������졢��־�� GitHub��

**�Ѿ��� root�������ն���ʾ `root@������:~#`��ʱ��ֱ������ `python3`������Ҫ `sudo`��Ҳ����Ϊ�˰�װ `sudo`��** ��ͨ�û���ʹ�������õ� `sudo`�����û�У���ִ�� `su -` �л��� root��

һ���°�װ Linux ��Ĭ��Ŀ¼�� `/opt/starhub-control`��root �û�ֱ��ִ�У�

```bash
python3 /opt/starhub-control/admin-token.py --env /opt/starhub-control/control.env
```

��ͨ�û���ϵͳ�Ѱ�װ������ʹ�� `sudo`����

```bash
sudo python3 /opt/starhub-control/admin-token.py --env /opt/starhub-control/control.env
```

����Ѿ������ĵ����ű����ص� `/tmp/starhub-admin-token.py`��root �û������������أ�

```bash
python3 /tmp/starhub-admin-token.py --env /opt/starhub-control/control.env
```

ͨ����������Զ����� root / ��ͨ�û������Զ�ʶ��Ĭ�ϵ��¾ɰ�װ���ã�û�� `sudo` ����ͨ�û���õ���ȷ��ʾ��

```bash
(
  set -eu
  if [ "$(id -u)" -eq 0 ]; then
    token_reader=python3
  elif command -v sudo >/dev/null 2>&1; then
    token_reader='sudo python3'
  else
    printf '%s\n' '��ǰ���� root ��δ��װ sudo������ִ�� su - �л��� root�������б����' >&2
    exit 1
  fi
  token_script=$(mktemp /tmp/starhub-admin-token.XXXXXX)
  trap 'rm -f "$token_script"' EXIT
  curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/admin-token.py -o "$token_script"
  if [ "$token_reader" = python3 ]; then
    python3 "$token_script"
  else
    sudo python3 "$token_script"
  fi
)
```

�� `/opt/harbor` ���𲼾ֵ� root �����ͨ�û��� `python3` ǰ�� `sudo`����

```bash
python3 /opt/harbor/admin-token.py --env /etc/harbor/control.env
```

Դ�� / Compose / �Զ��� `--prefix` ��װ������������ռλ·���滻��ʵ�ʵ�**����·��**����Ҫ�� `/root` ֱ������ `python3 scripts/admin-token.py`�������·������Դ����ĿĿ¼����Ч��

```bash
python3 /ABSOLUTE/PROJECT/scripts/admin-token.py --env /ABSOLUTE/PRIVATE/.env
```

`sudo: command not found` ��ʾϵͳû�а�װ `sudo`��root �û�ȥ�������ɣ�`can't open file '/root/scripts/admin-token.py'` ��ʾ��ǰĿ¼��û�и�Դ��ű�������ʾ�޷�Ψһ��λ���ã�ʹ�� `--env` ָ����̨���ʵ�ʵ� `control.env` ��˽�� `.env`����Ҫ�½����ơ�

## Windows һ��ж��

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -OutFile "$env:TEMP/starhub-uninstall.py"; python "$env:TEMP/starhub-uninstall.py" --apply
```

## Windows ��ȡ������Ա��¼����

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/admin-token.py -OutFile "$env:TEMP/starhub-admin-token.py"; python "$env:TEMP/starhub-admin-token.py"
```

Nginx / ����վ�㡢Docker �����������������������ж�ء�����˽������·����������ָ��� [��װ�ĵ�](docs/INSTALL.md)��

## 0.13 ����������

- Э��/����/��ȫ�ֲ㣬���ں�����У�飻���� HTTP/SOCKS/Mixed/Hysteria v1/Naive �� HTTPUpgrade/XHTTP/HTTP2/mKCP ���䣬������Χ����������
- Telegram ���ܰ󶨡�ֻ��������Ȩ����ʱ��������ѡ�����¼����ѣ�Ĭ�Ϲرա�
- ����ǽ�汾�༭������վɾ��/�ָ��������ܿ��Ƴ���ȡ�������������ع���
- ԭ������ʶ��ݸ塢�ͻ���·�����ü�顢���ӻ������Զ�ͬ����Ƕ����չ��ȥ�ء�
- ����Ȩ�˺Ų��ٿ��ܱ�Ϊ����֤�������˿�ʧ��ʱ������ȫδ�����Ч��վ��������չЭ����û������������־������
- ������������°�װ Agent Ϊ **0.25.5**������������λ����ֻ�����������棬���� 0.23.17 Agent �ɼ���ʹ�á�
- [0.13 ���ܡ����á�֪ͨ����ݱ߽�](docs/WORKFLOW-v013.md)��

## 0.12 ����

- ���ֽڵ㷢����ַ�� VPS ��վ���ͣ�IPv4 / IPv6 ��ѡ��Ĭ��ʹ����ѡ���� IP���� IPv6 ʱ��ѡ��˫��ַ����ͬһ����վ���̡�
- �û���ͳһ��Ȩ SS / Snell ������û�Э�顣������Կ����ڼ������޷�αװ�ɰ��û��޶�����������
- �û� + �ͻ��˷������ԣ�Mihomo YAML / ���ӻ��༭���ȶ��ڵ����á���Э���� IP ���Ͷ�̬ƥ�䣬�Զ�ת sing-box / Xray��
- Hiddify��NekoBox��v2rayNG��v2rayN��Clash Verge �ȿͻ���ѡ����������Я������URI ���ڵ㣬��ʽɸѡ����Э�顣
- ÿ��ָ�������ã����� 5 �� �� ���� 5 �գ�����ĩȱ��ȡ���һ�죬��������ԭ�趨�ա�
- ��ѡ��̨������IP + �˿ڡ���������������Ĭ�϶�����ڰ��������� �� ��վ���� �� IP ���ˡ�
- Linux Agent 0.12 �ܿ�Զ��ж�أ��ȴ���ʵ��ִ���������ƶ����ݣ�����ж�ؿ����桢���ݿ������������
- [���湤����������](docs/WORKFLOW-v012.md)��

## 0.11 ����

- Э���ѡ / ȫѡ�����ں�Э�飬ͬ����ԭ�ӱ��棬�˿��Զ����䣬���Բ��ظ�������
- ����������ÿ��һ�����ã�֧���Զ���������㣻Clash/Mihomo ԭ�� YAML ������������Ӧͷ��
- VPS Э����� JSON �༭���ͻ��� JSON/YAML ģ��༭�����İ��û��޸ĺ�������ϡ�
- ����Ʒ������޸�վ��������������Ȩ API ��Կ����Ч�ڡ�����������������ǰ�� CORS��
- [API �뼯��˵��](docs/API-INTEGRATIONS.md) �� [OpenAPI](docs/openapi.json)����վ�������ܿ�ж����Ҫ���� Agent 0.12������Э��ɼ������С�

## ���湦��

- �����༭��ͣ�ú�ɾ�������� 15 ���� Agent �������Agent �������������迪��Ŀ���豸�����˿ڡ�
- ÿ̨����ÿ���ں˺ϲ�����һ�����̣�VLESS��VMess��Trojan��Hysteria2��TUIC��AnyTLS��Snell��Shadowsocks ģ�壬����վ�༭����ͣ��ȷ��ֹͣ��ɾ����
- �����û����ݡ��û��顢���ڡ�������ȡ�ÿ�� / ÿ�� / �ֶ����á���Ⱥľ�����������������ڰ����ͳ�ơ�
- sing-box��Xray JSON��Mihomo��Shadowrocket ��ʽѡ�񣻿ͻ�������ģ�塢�������¸��ơ��ֻ��ͳ������ͻ���������֧��ʱ��ȷ�ܾ�������
- IPv4 / IPv6��FRP TCP ��͸������ 8 ����ʽ���������� HTTPS����� ACME ֤�顢���޷������ء�����ǽԤ���ͱ�������Ӧ�ù��ߡ�
- ���� / ϵͳ���⡢�ƶ��˲��֡�������أ�����Ԫ���ݱ��� 7 �� / 20,000 ��������¼ HTTPS ҳ�����ݡ�

## ��������

��Ҫ Docker Engine + Compose v2��Windows ʹ�� Docker Desktop Linux ����ģʽ���� Python 3��

```bash
bash scripts/install.sh compose
```

```powershell
.\scripts\install.ps1 -Mode compose
```

�� http://127.0.0.1:5173��ʹ�ñ��� `.env` �� `ADMIN_TOKEN`����װ������ȱʧ���ƾ�ݲ��������� `MASTER_KEY`��API�������ˡ�ǰ�˽��󶨻ػ������ݿⲻ���������˿ڡ�Զ�̹��������� HTTPS ���ػ� SSH ת������� [��װ](docs/INSTALL.md)��

```bash
ssh -N -L 5173:127.0.0.1:5173 YOUR_USER@YOUR_VPS
```

## �ֶ����������

Դ�빹��ʹ�� Go **1.27.3+**��Node.js 22.12+��Python 3��ԭ����������Ҫ PostgreSQL 17��������ɺ����� Node ��פ��

```bash
bash scripts/install.sh build
python scripts/prepare.py
# ��˽�� .env ���� DATABASE_URL �� WEB_DIR��web/dist ����·����
bash scripts/run-native.sh control
```

Windows ʹ�� `./scripts/install.ps1 -Mode build` �� `./scripts/run-native.ps1 -Role control`��Linux / Windows amd64 / arm64 ����ƽ̨������ GitHub Build ��������Ŀ�����װϸ�ڼ� [INSTALL.md](docs/INSTALL.md)��

```bash
go vet ./...
# ���ɲ���ֻʹ��ר�ò��Կ⣬�Զ����� / �������� schema
export TEST_DATABASE_URL='postgres://TEST_USER:TEST_PASSWORD@127.0.0.1:5432/harbor_test?sslmode=disable'
go test -count=1 ./...
python scripts/test_domain_sync.py
python scripts/test_managed_gateway.py
python scripts/test_agent_bootstrap.py
cd web && npm ci && npm test && npm run build
```

## �ں���һ�������

��װ�����ù̶����ص�ַ�� SHA-256 У�顣����ʹ�ùٷ� sing-box 1.14.4��Xray 26.10.21��FRP 0.71.1 �����ļ�������ʱ�˶Թٷ��ʲ� SHA-256������ʹ�ñ���Ŀ���ƺ�׺��������Ƭ�ġ��ں˰汾�����Բ�ѯ�����ȶ��桢��װ���л�ָ���汾��Agent �ȼ�鵱ǰ���ã�����ʧ��ʱ���ˡ�

```bash
python scripts/fetch-engines.py
python scripts/build-agent-releases.py --output agent-releases-v010
```

fetch-engines.py ���عٷ������ļ������� Go��ʹ�� --sing-box-version / --xray-version / --frp-version ָ����ǩ�� latest���ű����Ḳ������ engines/��Agent ������δ�޸ĵĹٷ� sing-box ��ӦԴ�������ɡ��ٷ���Դ��ժҪУ�鲻��ͬ��û������©���������������ע�ٷ���ȫ���档`AGENT_RELEASE_DIR` ָ���°汾Ŀ¼����������Ŀ�� VPS �Ľ�������������Ѱ�װ�� Agent��

## ʹ����߽�

��ϵͳ�����޸����� / HTTPS������ʵ��״̬���лر�����ͨ TLS ��Ҫ�Լ�������Ӧ��֤�飻**ֻ�� REALITY ��Ŀ���� SNI ����ѡ�󳧺�ѡվ��**�������Ŀ�� VPS ��֤�ɴ��ԡ�֤�鲻������������������VMess AEAD ������ǿ��֤�飬�Ƿ���Ҫ֤��ȡ������� TLS��

����������� 6 �־��ж����û���֤��ģ�壻Snell / ��ǰ Shadowsocks ģ��ʹ�ù�����Կ��ֻ�ܰ���ڼ�����ͳ��Լ 10 ���ϱ�����ʱ������쳣�˳�ʱ������������Կ��ܳ��֡�������������ʼ����ʷ�޷����㡣��� [����](docs/TRAFFIC-v010.md)��

�޹��� IPv4 �豸���������ӿɴ��м̣�IPv6-only ���Ҫ��ͻ��˾߱� IPv6 �ɴ��ԡ�ϵͳ���ǵ�����Ա����ƽ̨��������ҵ���⻧���롢MFA / OIDC��֧���������ָ��������������ز��ԡ���ȫɨ�費���ڲ�����©������ [��ȫ���](docs/SECURITY-v010.md)��

���� URL ���������ջ������صĴ���ƾ�ݣ���Ҫ�����û����ֻ��û�ƾ�ݻ�ͣ�ö�Ӧ��ڡ����ݿ�� `MASTER_KEY` ����ֱ𱸷ݣ���Կ�����ύ GitHub��

## �ĵ�

- [���湦�������](docs/FEATURES.md)
- [�������������ʼ�¼](docs/TRAFFIC-v010.md)
- [Э�� / CRUD ������](docs/WORKFLOW-v09.md)
- [��ȫ������ں˹���](docs/SECURITY-v010.md)
- [��������](docs/AGENT-CONNECT.md) �� [���� / NAT / IPv6](docs/PUBLISH.md)
- [����ǽ](docs/FIREWALL.md) �� [OpenAPI](docs/openapi.json)
- [����·��](docs/ROADMAP.md)


## ж�������Ա����

Linux / Windows ͨ�����������ǰ�������಼�ּ� [��װ�ĵ�](docs/INSTALL.md)���������������� ADMIN_TOKEN��

## ���ƶ˻������� Agent ��ַ�޸�

��ϵͳ���� �� վ�����������ɵ���������� 4 �� Agent HTTPS ���Ƶ�ַ�������ȼ�������ַ��������ӵ����Ч֤�飬��������ͬһ������ API����������Զ����������� Nginx ���ء����ո������̨������������������ά����

�ȱ��־������ɴ���ò���֤�µ�ַ��Agent **0.23.17+** ���Ѽ�Ȩ������ȡ��ַ��ͨ����Ŀ�������������ս�� HMAC ֤�����л��������� HTTP �ض��򡢲��ƹ� TLS������δ����֤��Ŀ�귢�����ơ�����֤��ַ������˽��״̬Ŀ¼��������ɻָ������ʱ�뱣������ڣ���ȫ�������ġ����Ƶ�ַ���ر����º��ٹرա�

��ԭ��ַ��ʧЧ�����롸�������� �� ���� / �ָ� �� ���ɿ��ƶ˵�ַ�޸��������ԭ VPS SSH ִ�С����� HOST_ID / AGENT_TOKEN���ںˡ�Э�����ú������˱�������ɾ������������ע�ᡣ������ Agent ���������У�鱾���ƶ˰汾��Ӧ�� GitHub ��ʽ���а��������±�׼ Linux `/opt/harbor-agent` ��װ�� Agent��ʧ�ָܻ��ɶ����ƣ������ں˲��л����ܹ������ľɲ��� Agent �������������ά�������ֲ��������ܶ����жϱ����йܴ�����

## ���ݿ��ڴ�

�ڴ����޲���Ԥռ�ڴ档�°�װ�� Compose Ĭ�� 512 MiB��Compose �� `POSTGRES_MEMORY_LIMIT`���°�װ�� `quickstart.py --db-memory-mb 512` �������� Linux ��װ�ĸ������ֽ������С���ޣ������ڴ� ��1.5 GiB ʱΪ 512 MiB����768 MiB ʱΪ 384 MiB������Ϊ 256 MiB������ԭ�и�����޶����ã����������ݿ⡢���������ݡ����Զ����������������� �� ��̨�汾�ɲ鿴����ر��������������ڴ档

��Ҫ��������ʱ���� SSH ִ�У�ֻ�����������ޣ���

```bash
sudo python3 /opt/harbor/scripts/panel-updater.py --env /etc/harbor/control.env --database-memory-mb 768
```

�°�װʹ�� `/opt/starhub-control/scripts/panel-updater.py --env /opt/starhub-control/control.env`����ʮ̨ VPS / �ϰ��û����ǵ����ڴ��Ⱦ��ܱ�֤�������������۲��ֵ��OOM����Ӧ�ӳ١����̡������ϱ��뱸�ݣ�Ĭ��Ӧ�����ӳ� 4 �����������ӣ�VPS �����������ݿ���������

�˿���Ծ��Hysteria / HY2 �Ŀ�ѡ���á�Linux �������ֺͿͻ��˸�ʽ˵���� [PORT-HOPPING.md](docs/PORT-HOPPING.md)�����нڵ�Ĭ�Ϲرգ�Զ��������Ҫ��ִ������ṩ�����ְ�װ / �������

HY2 ����˴�����ͻ��˴����ɷֱ����ã�TUIC / AnyTLS ר���ͻ��˲���Ҳ֧����������ͬ�����÷�����λ������Э�鷶Χ���������Ƽ� [Э���������������](docs/PROTOCOL-PERFORMANCE.md)��
