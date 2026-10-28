自动准备环境的一键入口见 [README](../README.md)；以下仍提供手动 / 原生 / 离线方式。自动依赖行为和 Windows 重启边界见 [0.21 说明](WORKFLOW-v021.md)。

# 安装与部署

**给现有总管理端添加其他 VPS：优先使用 [面板一键连接向导](AGENT-CONNECT.md)。** 下文的控制端安装和手动 Agent 配置适用于新建总管理端或高级场景。

## 预编译新总管理端一键安装

README 的 Linux / Windows 命令下载 GitHub 固定版本安装入口，检查平台包 SHA-256，调用 scripts/quickstart.py。需要 Python 3 和已经可用的 Docker；Docker Desktop 要使用 Linux 容器。不会替你关闭 Windows 执行策略；被组织策略拦截时使用已允许的签名或手动方式。

默认安装到 Linux /opt/starhub-control 或 Windows ProgramData/StarhubControl，拒绝覆盖。默认仅回环访问，--public 明确开放 IP:18080 的认证网页；HTTP 凭据会明文传输。可用 --origin 指定外部入口，不会自动创造公网 IP 或配置 DNS/TLS。安装后可在系统设置保存主站 / 独立订阅域名；自动申请与切换证书还需按域名网关章节安装 root 同步器。Windows 使用当前用户登录任务，退出登录后不保证运行；Linux 非 systemd 用 --foreground 接入 OpenRC 或自己的监督器。

控制面原生运行，数据库单个容器约束内存，内部端口全部回环。完整 Agent 包会核验下载，面板一键接入 Linux VPS 无需目标机安装数据库。离线安装可预先准备二进制包及 Agent 分发包；quickstart.py 的自动模式需要下载，完全离线请按下面原生手动方式设置 AGENT_RELEASE_DIR。

Debian/Ubuntu 的 Python 可由发行版仓库安装：apt install python3 curl；Fedora/Rocky/Alma 用 dnf install python3 curl；openSUSE 用 zypper install python3 curl；Alpine 用 apk add python3 curl bash，采用前台/OpenRC。Docker 依赖按官方对应发行版安装，不覆盖现有运行环境。

## 支持系统与方式

|方式|系统|前提|
|---|---|---|
|Compose|Ubuntu/Debian、Rocky/Alma/Fedora、openSUSE、Alpine|已有官方Docker Engine、Compose v2、Python3、bash；Alpine另装bash|
|Compose|Windows 10/11等受支持Desktop系统|Docker Desktop使用Linux容器；Windows Server使用受支持的Linux VM|
|源码原生|Linux/Windows amd64、arm64|Go1.27.3+、Node22.12+；控制面运行还需PostgreSQL17|
|预编译原生|Linux/Windows amd64、arm64|手动GitHub Build工作流；控制面需PostgreSQL，Agent不需要|
|常驻Agent|systemd Linux / Windows桌面|普通用户systemd服务 / 当前用户登录计划任务|

脚本不静默升级系统Docker、数据库或覆盖已有服务。Docker安装参考 <https://docs.docker.com/engine/install/>。

## Compose一键和手动

```bash
bash scripts/install.sh compose
# PowerShell： ./scripts/install.ps1 -Mode compose
```

手动等价命令：

```bash
python scripts/prepare.py
docker compose config --quiet
docker compose build
docker compose up -d --wait
```

不要先复制含空关键字段的.env.example；prepare生成缺失值但拒绝已有空密码。保留.env、MASTER_KEY和PG卷。升级前 `python scripts/backup.py harbor-before-upgrade.dump`，另行安全备份.env；不要运行 `docker compose down -v`。恢复在全新隔离库演练，参考OPERATIONS.md。

离线：同平台联网机构建，`docker compose config --images` 查询镜像，使用docker save/load迁移，再 `docker compose up --no-build -d --wait`。源码、私有.env和数据库数据分别迁移；不向GitHub上传凭据。

## 原生构建与运行

```bash
bash scripts/install.sh build
# PowerShell： ./scripts/install.ps1 -Mode build
python scripts/prepare.py
```

**预编译包跳过 build 步骤，直接运行 `python scripts/prepare.py`；使用 `bin/` 和随附的 `web/dist`。Compose 必须下载完整源码包，二进制包不含 Compose 构建上下文。**

为本机PostgreSQL创建独立harbor用户和数据库，用随机密码，仅监听回环。给私有.env添加：

```dotenv
DATABASE_URL=postgres://harbor:YOUR_PASSWORD@127.0.0.1:5432/harbor?sslmode=disable
WEB_DIR=/ABSOLUTE/PROJECT/web/dist
HTTP_ADDR=127.0.0.1:8080
SHARE_ADDR=127.0.0.1:8091
GOMEMLIMIT=128MiB
```

Windows WEB_DIR示例 `D:/Harbor/web/dist`。远程数据库要验证TLS，disable只限同机回环。然后：

```bash
bash scripts/run-native.sh control
# PowerShell： ./scripts/run-native.ps1 -Role control
```

控制面自动迁移，提供回环8080 API、5173 UI（设置WEB_DIR后）及8091订阅处理器。原生 UI 默认回环 5173，可通过 WEB_ADDR 明确指定 IP:端口，UI 代理到本机 8080 和 /s/ 的 8091。PUBLIC_ORIGIN 仅在首次安装初始化站点入口。原生UI固定代理到本机8080，因此此方式保留默认API端口。生产无需Node常驻；若写系统服务，使用专用普通用户、EnvironmentFile、Restart=on-failure、NoNewPrivileges=true，ExecStart指向harbor-control。开发服务器不用于生产。

离线源码需提前准备Go/npm缓存，或迁移对应平台的二进制和web/dist。GitHub构建产物仅供原生运行，不包含完整Compose构建上下文。

## Agent：Linux / Windows

每台设备使用独立引擎目录。下载脚本核对官方 sing-box / Xray / FRP 的资产 SHA-256，只提取白名单二进制，不执行自定义构建、不解压任意文件路径。可用 --sing-box-version / --xray-version / --frp-version 指定版本或 latest（最新稳定版）。主机页面的内核版本按钮可通过 Agent 进行原生检查与版本切换。

```bash
python scripts/fetch-engines.py
# 可选 --os linux --arch arm64，为另一平台准备离线包
python scripts/enroll-agent.py --control https://vps.example.com --name device-a --provider self --address YOUR_INVENTORY_ADDRESS --live
```

隐藏输入管理员令牌，管理员令牌不保存。脚本登记主机、保存一次性凭据和引擎路径/摘要到.agent.env；已有该文件则拒绝覆盖。Linux目录0700、凭据0600；Windows限制整个项目和状态目录的NTFS ACL到本人及管理员，不放共享目录。离线迁移引擎时保留receipt.json并核对架构。

```bash
bash scripts/run-native.sh agent
# PowerShell： ./scripts/run-native.ps1 -Role agent
```

Linux常驻：`sudo bash scripts/install-agent-linux.sh`。拒绝覆盖旧服务；引擎复制到root只读/opt/harbor/engines，状态/var/lib/harbor/live由普通harbor用户写入。查看 `journalctl -u harbor-agent`。Alpine/OpenRC可使用前台命令，由管理员按发行版配置守护服务。

Windows常驻：`./scripts/install-agent-windows.ps1` 创建当前普通用户的HarborAgent登录计划任务，不提权、不绕过执行策略。用户未登录或退出后不保证常驻，不冒充Windows系统服务。修改凭据后重启任务；企业策略阻止脚本时采用组织允许的签名方式。Windows防火墙规则由管理员按实际端口配置，脚本不自动放行。

只有ENABLE_LIVE=1才协调真实服务。进程失败退避重试，控制面失联90秒停止受管进程。Linux父进程死亡信号、Windows Kill-on-close JobObject负责清理子进程。不要多个Agent共用同一状态目录。

## 已有nginx / 宝塔域名网关

不要安装第二个nginx或抢占宝塔80/443。先检查 `nginx -T`，确认没有其他同名server_name；工具不会删除旧站点。

1. DNS A指向VPS；IPv6用可达AAAA。HTTP-01验证要求公网80可达，所有A/AAAA正确。普通Shadowsocks/FRP TCP地址使用DNS-only或IP，不走普通HTTP CDN。
2. 在「域名与分享」保存域名及HTTPS，最初通过SSH转发管理。
3. 安装发行版certbot；审查domain-sync.py，将同步器与构建的harbor-gateway放到root只读/opt/harbor。私有/etc/harbor/control.env只需ADMIN_TOKEN，权限0600。
4. 把命令中的YOUR_ADMIN_IP替换为实际管理员来源，不能使用/0。

```bash
sudo python3 /opt/harbor/domain-sync.py \
 --env /etc/harbor/control.env \
 --gateway /opt/harbor/harbor-gateway \
 --nginx /www/server/nginx/sbin/nginx \
 --out /www/server/panel/vhost/nginx/harbor-vps.conf \
 --admin-cidrs YOUR_ADMIN_IP/32,127.0.0.1/32,::1/128 \
 --issue --apply
```

普通nginx使用/usr/sbin/nginx与/etc/nginx/conf.d/harbor.conf。首次输出文件必须不存在，后续必须带Harbor标记。工具验证权限、证书域名/有效期、候选nginx配置，成功后重载，失败恢复原文件。ACME引导期只提供challenge，其余503，不短暂公开明文凭据。

--issue显式允许certbot申请、续期及其ACME条款；不使用此参数时证书需提前位于/etc/letsencrypt/live/DOMAIN。API与普通Agent不会得到root权限。

修改scripts/harbor-domain-sync.service的路径和管理员CIDR，复制service/timer到/etc/systemd/system，再 `systemctl daemon-reload` 和 `systemctl enable --now harbor-domain-sync.timer`。每分钟同步面板域名。默认拒绝降级HTTP；若明确需要面板开关HTTP，给服务命令额外加--allow-http。证书检查间隔12小时，未变化不重载，同一失败设置退避30分钟。

订阅/s/与Agent专用接口经网关访问，其他路径受管理员CIDR限制。管理员IP变动可通过SSH转发恢复。网关回报生效表示本地检查/重载成功，不表示公网端口、云安全组已验证。

## 停止与恢复

- Compose stop/down保留PG卷；不要加-v。
- Linux Agent：systemctl stop harbor-agent；Windows停止HarborAgent任务或前台Agent，会清理受管引擎。
- 域名失败检查journalctl、DNS、证书和nginx -t。修复后可删除该自有vhost的.state.json解除退避，不删除其他站点。
- 恢复必须同时具备数据库与原MASTER_KEY；生成新密钥不能解密旧服务。


## 获取管理员登录令牌与卸载

只在自己的本机 / SSH 终端查看已有令牌，不要把输出上传到 GitHub、日志或聊天。不会重置令牌：

```bash
# 已有星枢管理安装：自动定位控制面配置（不是宝塔令牌）
sudo python3 /opt/harbor/admin-token.py
# 一键新装 Linux 布局
sudo sed -n 's/^ADMIN_TOKEN=//p' /opt/starhub-control/control.env
# 源码 / 自选私有环境文件：只打印 ADMIN_TOKEN
python3 scripts/admin-token.py --env /ABSOLUTE/PRIVATE/.env
```

Windows PowerShell（当前用户一键安装）：

```powershell
Get-Content -LiteralPath "$env:ProgramData\StarhubControl\control.env" | Where-Object { $_.StartsWith('ADMIN_TOKEN=') } | ForEach-Object { $_.Substring(12) }
```

一键卸载面板运行组件，**默认保留数据库卷、私有配置和所有备份**。下面是项目脚本，下载后可先去掉 --apply 查看计划：

```bash
curl -fL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -o /tmp/starhub-uninstall.py && sudo python3 /tmp/starhub-uninstall.py --apply
```

Windows（需要 Python 3；只卸载所选安装目录和对应的当前用户任务）：

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -OutFile "$env:TEMP\starhub-uninstall.py"; python "$env:TEMP\starhub-uninstall.py" --apply
```

`--purge-data` 会**不可恢复地删除该项目数据库卷和私有配置**，必须自行确认备份；默认不执行。保留的数据会阻止新装脚本覆盖，恢复请走安装文档的备份恢复流程。宝塔已有域名反代在宝塔中单独关闭；Nginx、其他网站、Let's Encrypt、Docker、系统防火墙和其他 VPS 上的 Agent 不会被删除。先从面板逐台退役远程 Agent，再卸载总管理端。

[网络 / 封装语义与协议核验](PROTOCOLS-v019.md) · [完整卸载脚本](../scripts/uninstall.py)

当前订阅与客户端限制说明见 [0.20 工作流](WORKFLOW-v020.md)。Linux / Windows 通用安装、卸载、面板令牌命令置于 [README](../README.md) 前部。
