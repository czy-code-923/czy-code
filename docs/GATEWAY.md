# ������֤�������ذ�װ�޸�

`IP:18080` �ܷ���ֻ��ʾ�����������С�վ�����������֤�顢�����������Ҫ Nginx��Certbot��Cloudflare DNS �����������ܿ��������֡�0.25.5 �� Linux systemd һ����װ�Ჹ����Щ��������б��� / Nginx ���ȼ���ʶ�𣬱�������վ�㡣

## �ɰ� Linux ��岹װ��������װ��

�� root ִ�С���ͨ�û����л� root��root ����Ҫ sudo�������ȡ���е�˽�����ã������ɻ��������Ա���ơ��������ݿ�ʹ������ݣ�����Ҫ���κ����Ʒ��������С�

```bash
(
  set -eu
  f=$(mktemp)
  trap 'rm -f "$f"' EXIT
  curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/v0.25.5-distribution/scripts/install-gateway.py -o "$f"
  python3 "$f" --env /opt/starhub-control/control.env --install-deps --enable-acme
)
```

�ű����ع̶���ʽ��Դ������˶� GitHub ���������� SHA-256 �ʹ�С��ֻ��ȡָ�����������֡����� 0.25.x ��������Ȳ�װ�����������������棻���µ� 0.25.5 ��֤��ҳ������ʾ��������������״̬��

- Debian / Ubuntu �Զ�ʹ�� apt ��װȱʧ������dnf / yum ϵͳ��Ҫ�����ÿ��ṩ Certbot DNS ����ķ��а�ֿ⣻���������û�ֹͣ�����������档����ϵͳ���÷��а����������װ Nginx��Certbot �� Cloudflare DNS �������ȥ�� `--install-deps` ���ԡ�
- Ĭ����װ����Ϊ `/opt/starhub-control/control.env`���� `/opt/harbor` ���ָĳ� `--env /etc/harbor/control.env`��
- ��⵽�������еľ� `harbor-domain-sync.timer` �� `harbor-managed-gateway.timer` ʱ��ܾ����ӵڶ������֣�Ӧ����ά��ԭ���֡�
- ���б��� Nginx ʹ�� `/www/server/nginx/sbin/nginx` ���� vhost Ŀ¼������ϵͳ Nginx ʹ�õ�ǰ����� `/etc/nginx/conf.d`�����������з��������á�����ʶ��Ŀ¼����������ռ�� 80/443 ʱֹͣ���Ȱ�ʵ�ʷ����������ء�
- ������ҳ���δ�˽�������е�ʵ�� WEB_ADDR �˿ڶ�ȡ����װͨ��Ϊ 18080�����ݾɰ浥����ҳ����� 5173��
- �̳�����ԭ����ҳ������Χ���ѹ����Ŀ������ͨ�� HTTPS ����������¼�����ػ������İ�װ����������Դ���ơ��ڲ� API �����ݿ���ʹ�ûػ���ַ��
- `--enable-acme` ��ȷ����ִ��������Ѿ����õ�����ǩ�������ڡ�Cloudflare ����ֻ�����ں�̨�������ú�ǩ���ڼ��˽����ʱ�ļ��С�

## ����Ƿ�������

```bash
systemctl status starhub-gateway.timer --no-pager
journalctl -u starhub-gateway.service -n 60 --no-pager
```

����Լÿ��������һ�֡�֤��ԭ�ȴ��ڡ��ȴ�ǩ����ʱ���Զ�����������ɾ���ؽ���ʧ��ʱ�����־��������飬���������� DNS ƾ�ݺ������ԣ�֤����������̼�����ơ�

## DNS �� Cloudflare

- HTTP-01 Ҫ����ȷ�� A / AAAA ��¼����֤��ڹ��� TCP 80 �ɴ��� 18080 �˿ڲ��ܴ��� ACME ��֤�˿ڡ�
- Cloudflare DNS-01 ��Ҫ����������С���ơ���Ҳ��Ҫ�� DNS ָ���ܹ�����������Ҫ������ʹ�� Cloudflare DNS����Ӧ����� DNS �༭Ȩ�����Ƽ� Certbot DNS �����
- ֤��ǩ���ɹ�����վ�ɷ�������������վ��ʹ�� Cloudflare ������Դվ 80/443��Nginx ��ƥ��������֤��������ȷ���á�Դվ HTTPS ������ʹ�� Full (strict)����Ҫ�� Flexible ��Դ��ǿ�� HTTPS ��վ�㡣
- ��վ��������������ɷֱ����ã����ֻ�ֱ����������ܹ� Cloudflare ֤������������Ტ������Ϊ HTTP-01 ���롣

## ��ȫ��ж�ر߽�

�޸�����װ�ܿ����֣�������Զ������ Shell ��ԭʼ Nginx ָ�ǩ������ִ�У��������� Nginx ԭ����飻ʧ�ָܻ������ļ�����̨�����ڼ����ͣƽ̨�Լ����������֣���������ļ���ǩ��������

���ж�ػ�ֹͣ���Ƴ��������� service/timer������ Nginx ������������վ�� Let's Encrypt ֤�顣ж��ǰ�� Nginx / �����е���ȷ�ϲ���ʹ�õ����������ڡ�
