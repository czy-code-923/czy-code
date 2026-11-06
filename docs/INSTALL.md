�Զ�׼��������һ����ڼ� [README](../README.md)���������ṩ�ֶ� / ԭ�� / ���߷�ʽ���Զ�������Ϊ�� Windows �����߽�� [0.21 ˵��](WORKFLOW-v021.md)��

# ��װ�벿��

**�������ܹ������������� VPS������ʹ�� [���һ��������](AGENT-CONNECT.md)��** ���ĵĿ��ƶ˰�װ���ֶ� Agent �����������½��ܹ����˻�߼�������

## Ԥ�������ܹ�����һ����װ

README �� Linux / Windows �������� GitHub �̶��汾��װ��ڣ����ƽ̨�� SHA-256������ scripts/quickstart.py����Ҫ Python 3 ���Ѿ����õ� Docker��Docker Desktop Ҫʹ�� Linux ��������������ر� Windows ִ�в��ԣ�����֯��������ʱʹ����������ǩ�����ֶ���ʽ��

Ĭ�ϰ�װ�� Linux /opt/starhub-control �� Windows ProgramData/StarhubControl���ܾ����ǡ�Ĭ�Ͻ��ػ����ʣ�--public ��ȷ���� IP:18080 ����֤��ҳ��HTTP ƾ�ݻ����Ĵ��䡣���� --origin ָ���ⲿ��ڣ������Զ����칫�� IP ������ DNS/TLS����װ�����ϵͳ���ñ�����վ / ��������������0.25.5 �� Linux systemd һ����װ�Ჹ�� Nginx��Certbot / Cloudflare DNS ������ܿ��������֣��ɰ��Ѱ�װ��尴 [�����޸�](GATEWAY.md) ��װ���������������ݿ⡣Windows �ͷ� systemd ��װ��Ҫ�������� Linux ���ء�Windows ʹ�õ�ǰ�û���¼�����˳���¼�󲻱�֤���У�Linux �� systemd �� --foreground ���� OpenRC ���Լ��ļල����

������ԭ�����У����ݿⵥ������Լ���ڴ棬�ڲ��˿�ȫ���ػ������� Agent ����������أ����һ������ Linux VPS ����Ŀ�����װ���ݿ⡣���߰�װ��Ԥ��׼�������ư��� Agent �ַ�����quickstart.py ���Զ�ģʽ��Ҫ���أ���ȫ�����밴����ԭ���ֶ���ʽ���� AGENT_RELEASE_DIR��

Debian/Ubuntu �� Python ���ɷ��а�ֿⰲװ��apt install python3 curl��Fedora/Rocky/Alma �� dnf install python3 curl��openSUSE �� zypper install python3 curl��Alpine �� apk add python3 curl bash������ǰ̨/OpenRC��Docker �������ٷ���Ӧ���а氲װ���������������л�����

## ֧��ϵͳ�뷽ʽ

|��ʽ|ϵͳ|ǰ��|
|---|---|---|
|Compose|Ubuntu/Debian��Rocky/Alma/Fedora��openSUSE��Alpine|���йٷ�Docker Engine��Compose v2��Python3��bash��Alpine��װbash|
|Compose|Windows 10/11����֧��Desktopϵͳ|Docker Desktopʹ��Linux������Windows Serverʹ����֧�ֵ�Linux VM|
|Դ��ԭ��|Linux/Windows amd64��arm64|Go1.27.3+��Node22.12+�����������л���PostgreSQL17|
|Ԥ����ԭ��|Linux/Windows amd64��arm64|�ֶ�GitHub Build����������������PostgreSQL��Agent����Ҫ|
|��פAgent|systemd Linux / Windows����|��ͨ�û�systemd���� / ��ǰ�û���¼�ƻ�����|

�ű�����Ĭ����ϵͳDocker�����ݿ�򸲸����з���Docker��װ�ο� <https://docs.docker.com/engine/install/>��

## Composeһ�����ֶ�

```bash
bash scripts/install.sh compose
# PowerShell�� ./scripts/install.ps1 -Mode compose
```

�ֶ��ȼ����

```bash
python scripts/prepare.py
docker compose config --quiet
docker compose build
docker compose up -d --wait
```

��Ҫ�ȸ��ƺ��չؼ��ֶε�.env.example��prepare����ȱʧֵ���ܾ����п����롣����.env��MASTER_KEY��PG��������ǰ `python scripts/backup.py harbor-before-upgrade.dump`�����а�ȫ����.env����Ҫ���� `docker compose down -v`���ָ���ȫ�¸�����������ο�OPERATIONS.md��

���ߣ�ͬƽ̨������������`docker compose config --images` ��ѯ����ʹ��docker save/loadǨ�ƣ��� `docker compose up --no-build -d --wait`��Դ�롢˽��.env�����ݿ����ݷֱ�Ǩ�ƣ�����GitHub�ϴ�ƾ�ݡ�

## ԭ������������

```bash
bash scripts/install.sh build
# PowerShell�� ./scripts/install.ps1 -Mode build
python scripts/prepare.py
```

**Ԥ��������� build ���裬ֱ������ `python scripts/prepare.py`��ʹ�� `bin/` ���渽�� `web/dist`��Compose ������������Դ����������ư����� Compose ���������ġ�**

Ϊ����PostgreSQL��������harbor�û������ݿ⣬��������룬�������ػ�����˽��.env���ӣ�

```dotenv
DATABASE_URL=postgres://harbor:YOUR_PASSWORD@127.0.0.1:5432/harbor?sslmode=disable
WEB_DIR=/ABSOLUTE/PROJECT/web/dist
HTTP_ADDR=127.0.0.1:8080
SHARE_ADDR=127.0.0.1:8091
GOMEMLIMIT=128MiB
```

Windows WEB_DIRʾ�� `D:/Harbor/web/dist`��Զ�����ݿ�Ҫ��֤TLS��disableֻ��ͬ���ػ���Ȼ��

```bash
bash scripts/run-native.sh control
# PowerShell�� ./scripts/run-native.ps1 -Role control
```

�������Զ�Ǩ�ƣ��ṩ�ػ�8080 API��5173 UI������WEB_DIR�󣩼�8091���Ĵ�������ԭ�� UI Ĭ�ϻػ� 5173����ͨ�� WEB_ADDR ��ȷָ�� IP:�˿ڣ�UI ���������� 8080 �� /s/ �� 8091��PUBLIC_ORIGIN �����״ΰ�װ��ʼ��վ����ڡ�ԭ��UI�̶�����������8080����˴˷�ʽ����Ĭ��API�˿ڡ���������Node��פ����дϵͳ����ʹ��ר����ͨ�û���EnvironmentFile��Restart=on-failure��NoNewPrivileges=true��ExecStartָ��harbor-control������������������������

����Դ������ǰ׼��Go/npm���棬��Ǩ�ƶ�Ӧƽ̨�Ķ����ƺ�web/dist��GitHub�����������ԭ�����У�����������Compose���������ġ�

## Agent��Linux / Windows

ÿ̨�豸ʹ�ö�������Ŀ¼�����ؽű��˶Թٷ� sing-box / Xray / FRP ���ʲ� SHA-256��ֻ��ȡ�����������ƣ���ִ���Զ��幹��������ѹ�����ļ�·�������� --sing-box-version / --xray-version / --frp-version ָ���汾�� latest�������ȶ��棩������ҳ����ں˰汾��ť��ͨ�� Agent ����ԭ�������汾�л���

```bash
python scripts/fetch-engines.py
# ��ѡ --os linux --arch arm64��Ϊ��һƽ̨׼�����߰�
python scripts/enroll-agent.py --control https://vps.example.com --name device-a --provider self --address YOUR_INVENTORY_ADDRESS --live
```

�����������Ա���ƣ�����Ա���Ʋ����档�ű��Ǽ�����������һ����ƾ�ݺ�����·��/ժҪ��.agent.env�����и��ļ���ܾ����ǡ�LinuxĿ¼0700��ƾ��0600��Windows����������Ŀ��״̬Ŀ¼��NTFS ACL�����˼�����Ա�����Ź���Ŀ¼������Ǩ������ʱ����receipt.json���˶Լܹ���

```bash
bash scripts/run-native.sh agent
# PowerShell�� ./scripts/run-native.ps1 -Role agent
```

Linux��פ��`sudo bash scripts/install-agent-linux.sh`���ܾ����Ǿɷ������渴�Ƶ�rootֻ��/opt/harbor/engines��״̬/var/lib/harbor/live����ͨharbor�û�д�롣�鿴 `journalctl -u harbor-agent`��Alpine/OpenRC��ʹ��ǰ̨����ɹ���Ա�����а������ػ�����

Windows��פ��`./scripts/install-agent-windows.ps1` ������ǰ��ͨ�û���HarborAgent��¼�ƻ����񣬲���Ȩ�����ƹ�ִ�в��ԡ��û�δ��¼���˳��󲻱�֤��פ����ð��Windowsϵͳ�����޸�ƾ�ݺ�����������ҵ������ֹ�ű�ʱ������֯������ǩ����ʽ��Windows����ǽ�����ɹ���Ա��ʵ�ʶ˿����ã��ű����Զ����С�

ֻ��ENABLE_LIVE=1��Э����ʵ���񡣽���ʧ���˱����ԣ�������ʧ��90��ֹͣ�ܹܽ��̡�Linux�����������źš�Windows Kill-on-close JobObject���������ӽ��̡���Ҫ���Agent����ͬһ״̬Ŀ¼��

## ����nginx / ������������

�µ�һ����װ������岹װ����ʹ�� [���ذ�װ���޸�](GATEWAY.md)���������Ѿ����в����ͬ����ʱ���ֶ���ά˵������Ҫͬʱ�����������֡�

��Ҫ��װ�ڶ���nginx����ռ����80/443���ȼ�� `nginx -T`��ȷ��û������ͬ��server_name�����߲���ɾ����վ�㡣

1. DNS Aָ��VPS��IPv6�ÿɴ�AAAA��HTTP-01��֤Ҫ����80�ɴ����A/AAAA��ȷ����ͨShadowsocks/FRP TCP��ַʹ��DNS-only��IP��������ͨHTTP CDN��
2. �ڡ����������������������HTTPS�����ͨ��SSHת��������
3. ��װ���а�certbot�����domain-sync.py����ͬ�����빹����harbor-gateway�ŵ�rootֻ��/opt/harbor��˽��/etc/harbor/control.envֻ��ADMIN_TOKEN��Ȩ��0600��
4. �������е�YOUR_ADMIN_IP�滻Ϊʵ�ʹ���Ա��Դ������ʹ��/0��

```bash
sudo python3 /opt/harbor/domain-sync.py \
 --env /etc/harbor/control.env \
 --gateway /opt/harbor/harbor-gateway \
 --nginx /www/server/nginx/sbin/nginx \
 --out /www/server/panel/vhost/nginx/harbor-vps.conf \
 --admin-cidrs YOUR_ADMIN_IP/32,127.0.0.1/32,::1/128 \
 --issue --apply
```

��ͨnginxʹ��/usr/sbin/nginx��/etc/nginx/conf.d/harbor.conf���״�����ļ����벻���ڣ����������Harbor��ǡ�������֤Ȩ�ޡ�֤������/��Ч�ڡ���ѡnginx���ã��ɹ������أ�ʧ�ָܻ�ԭ�ļ���ACME������ֻ�ṩchallenge������503�������ݹ�������ƾ�ݡ�

--issue��ʽ����certbot���롢���ڼ���ACME�����ʹ�ô˲���ʱ֤������ǰλ��/etc/letsencrypt/live/DOMAIN��API����ͨAgent����õ�rootȨ�ޡ�

�޸�scripts/harbor-domain-sync.service��·���͹���ԱCIDR������service/timer��/etc/systemd/system���� `systemctl daemon-reload` �� `systemctl enable --now harbor-domain-sync.timer`��ÿ����ͬ�����������Ĭ�Ͼܾ�����HTTP������ȷ��Ҫ��忪��HTTP����������������--allow-http��֤������12Сʱ��δ�仯�����أ�ͬһʧ�������˱�30���ӡ�

����/s/��Agentר�ýӿھ����ط��ʣ�����·���ܹ���ԱCIDR���ơ�����ԱIP�䶯��ͨ��SSHת���ָ������ػر���Ч��ʾ���ؼ��/���سɹ�������ʾ�����˿ڡ��ư�ȫ������֤��

## ֹͣ��ָ�

- Compose stop/down����PG������Ҫ��-v��
- Linux Agent��systemctl stop harbor-agent��WindowsֹͣHarborAgent�����ǰ̨Agent���������ܹ����档
- ����ʧ�ܼ��journalctl��DNS��֤���nginx -t���޸����ɾ��������vhost��.state.json����˱ܣ���ɾ������վ�㡣
- �ָ�����ͬʱ�߱����ݿ���ԭMASTER_KEY����������Կ���ܽ��ܾɷ���


## ��ȡ������Ա��¼����

ֻ���Լ��ı��� / SSH �ն˲鿴��̨�������е� `ADMIN_TOKEN`�������Ǳ������ƣ��������ɻ�����ƾ�ݡ���������Ƶ������¼ҳ����Ҫ�ϴ��� GitHub����־�����졣

��ǰ��ʾ������ `root@������:~#`���Ѿ����� root Ȩ�ޣ�ֱ����������� `python3` ����ɣ�**����Ҫ��װ��ʹ�� `sudo`**����ͨ�û�������ǰ�� `sudo`��ϵͳû�� `sudo` ʱ������ `su -` �л� root�������� root ���

һ����װ Linux��Ĭ��Ŀ¼ `/opt/starhub-control`����

```bash
python3 /opt/starhub-control/admin-token.py --env /opt/starhub-control/control.env
```

�Ѿ����ع��ű��� `/tmp/starhub-admin-token.py`��

```bash
python3 /tmp/starhub-admin-token.py --env /opt/starhub-control/control.env
```

�� `/opt/harbor` ���𲼾֣�

```bash
python3 /opt/harbor/admin-token.py --env /etc/harbor/control.env
```

Դ�� / Compose / �Զ��尲װ���滻Ϊʵ�ʾ���·������

```bash
python3 /ABSOLUTE/PROJECT/scripts/admin-token.py --env /ABSOLUTE/PRIVATE/.env
```

`python3 scripts/admin-token.py --env .env` ֻ�������Ѿ�����Դ����ĿĿ¼��������� `/root` ִ��ʱ����� `/root/scripts/admin-token.py`����˱� `can't open file`����ʹ������ľ���·�������޸���һ����װ�� `--prefix`���ű��� `control.env` ��λ��ʵ�ʰ�װĿ¼�¡�

������ؽű������ڣ���ʹ�� [README ��ͨ����������](../README.md#linux-��ȡ������Ա��¼����)�������Զ��ж��Ƿ���Ҫ `sudo`����ʶ��Ĭ���¾ɲ��֡��Զ��廷���ļ���ͬʱ���ڶ����װ����ʱ���� `--env` ��ȷָ��Ŀ�ꣻ��Ҫ�����������ơ�

Windows PowerShell����ǰ�û�һ����װ����

```powershell
Get-Content -LiteralPath "$env:ProgramData\StarhubControl\control.env" | Where-Object { $_.StartsWith('ADMIN_TOKEN=') } | ForEach-Object { $_.Substring(12) }
```

## ж������������

һ��ж��������������**Ĭ�ϱ������ݿ����˽�����ú����б���**����������Ŀ�ű������غ����ȥ�� --apply �鿴�ƻ���root �û�ȥ�������е� sudo��

```bash
curl -fL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -o /tmp/starhub-uninstall.py && sudo python3 /tmp/starhub-uninstall.py --apply
```

Windows����Ҫ Python 3��ֻж����ѡ��װĿ¼�Ͷ�Ӧ�ĵ�ǰ�û����񣩣�

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -OutFile "$env:TEMP\starhub-uninstall.py"; python "$env:TEMP\starhub-uninstall.py" --apply
```

`--purge-data` ��**���ɻָ���ɾ������Ŀ���ݿ����˽������**����������ȷ�ϱ��ݣ�Ĭ�ϲ�ִ�С����������ݻ���ֹ��װ�ű����ǣ��ָ����߰�װ�ĵ��ı��ݻָ����̡������������������ڱ����е����رգ�Nginx��������վ��Let's Encrypt��Docker��ϵͳ����ǽ������ VPS �ϵ� Agent ���ᱻɾ�����ȴ������̨����Զ�� Agent����ж���ܹ����ˡ�

[���� / ��װ������Э�����](PROTOCOLS-v019.md) �� [����ж�ؽű�](../scripts/uninstall.py)

��ǰ������ͻ�������˵���� [0.20 ������](WORKFLOW-v020.md)��Linux / Windows ͨ�ð�װ��ж�ء���������������� [README](../README.md) ǰ����
