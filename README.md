# 星枢 · VPS 多机管理 0.25.6

修复 nftables 1.0.6 漏报表注释造成的端口跳跃节点“等待停止 / 无法删除”，保留规则归属校验；已有主机可单独更新网络辅助脚本，无需重启代理内核。[0.25.6 发布说明与旧主机修复](docs/RELEASE-v0256.md)。

保留已有协议、用户权限、流量计量、客户端策略与订阅功能。更新使用正式发布包、校验、停写备份和失败回退；控制面升级与远端 Agent 升级分开处理。

已修正 Android NekoBox / sing-box / Hiddify 订阅适配与规则集出站引用；增加受权限保护的订阅正文、节点与二维码 API；加固安装令牌；增加可选 SS 2022 / Snell 独立身份计量、证书编辑删除、公网 HTTPS 反代及流量查看。[0.23 功能与兼容边界](docs/WORKFLOW-v023.md)。

订阅节点的 IPv4 / 域名入口保留后台原名，IPv6 使用「原名-v6」。例如 `snell_pro` / `snell_pro-v6`，不附加随机 ID；策略仍按稳定端点引用选择，改名不会改变授权。重名、与策略组或客户端保留名称冲突时明确提示修改，不生成无法导入的配置。[本版说明](docs/WORKFLOW-v021.md)。

主流程：**连接主机 → 配置内核与协议 → 用户 / 用户组权限 → 客户端策略 → 用户原订阅分享**。使用 Clash Verge / FlClash 的设备名额时选择兼容模式；该模式统计订阅访问，不证明真实物理设备。已有链接自动跟随用户组授权，无须重建用户。

## 一键部署新总管理端

脚本检测 Python >=3.9、HTTPS 下载工具与 Docker Linux 容器引擎，缺少时自动安装，已有可用环境直接复用。预编译包按架构选择并核验固定 SHA-256，不需要 Go / Node。已有安装、数据卷、服务或端口冲突会停止，不能用此命令覆盖升级旧面板。

Linux（Debian 11+ / Ubuntu 22.04+、Fedora、Rocky / Alma / RHEL 8+ / CentOS Stream、openSUSE / SLES 的 systemd 系统；需 root 或 sudo）：

```bash
sh -c 'set -eu; S=; if [ "$(id -u)" != 0 ]; then command -v sudo >/dev/null || { echo "Run as root"; exit 1; }; S=sudo; fi; if ! command -v curl >/dev/null; then if command -v apt-get >/dev/null; then $S apt-get update; $S apt-get install -y curl ca-certificates; elif command -v dnf >/dev/null; then $S dnf install -y curl ca-certificates; elif command -v zypper >/dev/null; then $S zypper --non-interactive install curl ca-certificates; elif command -v apk >/dev/null; then $S apk add --no-cache curl ca-certificates; else echo "Install curl first on this OS"; exit 1; fi; fi; F=$(mktemp); curl --proto "=https" --tlsv1.2 -fL https://github.com/czy-code-923/czy-code/releases/download/v0.25.6-distribution/install-starhub.sh -o "$F"; $S sh "$F" --public; rm -f "$F"'
```

Windows 10/11 **管理员 PowerShell**：自动寻找或安装 Python、WinGet、Docker Desktop / WSL 组件，启动并等待 Linux 引擎。首次启用 WSL 需要重启时会提示，重启后重跑同一命令；不会自动重启、切换已有 Windows 容器或跳过 Docker 首次许可 / 虚拟化要求。Windows Server 使用 Linux 虚拟机。

```powershell
Invoke-WebRequest https://github.com/czy-code-923/czy-code/releases/download/v0.25.6-distribution/install-starhub.ps1 -OutFile "$env:TEMP/install-starhub.ps1"; & "$env:TEMP/install-starhub.ps1" -Public
```

默认公开 IP:18080 的认证网页（HTTP）；安装后可在设置绑定域名和 HTTPS。内部 API、分享处理器及数据库仍只监听回环。若要仅本机访问，去掉 `--public` / `-Public`。Windows 默认当前用户登录任务；Linux 服务由 systemd 管理。Alpine / OpenRC 可自动准备依赖，但面板用包内 `quickstart.py --foreground` 配合自己的监督器。环境安装不会卸载旧 Docker、清理镜像/数据或放开数据库端口。

[完整源码与四平台附件](https://github.com/czy-code-923/czy-code/releases/tag/v0.25.6-distribution) · [手动 / Compose / 离线安装](docs/INSTALL.md)

## 后台常驻与开机自启

一键新装 Linux 已使用 systemd，无需挂着 SSH 窗口。以 root 执行以下**完整单行命令**，启用并启动本项目已安装的组件：

```bash
( set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/panel-access.py -o "$f"; python3 "$f" enable-autostart --env /opt/starhub-control/control.env )
```

操作包括 Docker、归属校验通过的 PostgreSQL 容器重启策略、面板主服务、匹配的系统 Nginx，以及已安装的网关 / 更新助手 **timer**。宝塔 / 外部 Nginx 保留原监督方式；缺少助手会在 `missing_helpers` 列出，`complete=false`，不会假称已安装全部。不会开启临时恢复入口或改回普通网页的公网监听。其他 VPS 上的 Agent 由各自服务管理。

```bash
systemctl is-enabled docker.service starhub-control.service starhub-gateway.timer starhub-panel-updater.timer
systemctl status starhub-control.service starhub-gateway.timer starhub-panel-updater.timer --no-pager
```

定时助手的 `.service` 显示 `static` 是正常现象，应启用对应的 `.timer`。详细检查、数据库策略和缺少组件的处理见 [后台常驻与开机自启](docs/PANEL-ACCESS.md#后台常驻与开机自启)。

## 后台版本与自动更新

进入「系统设置 → 后台版本」，检查 GitHub 最新正式版，确认后提交更新。支持的 Linux systemd 安装由独立本机助手处理，关闭网页不会取消任务。更新包校验失败保留原版本；应用后健康检查失败则恢复数据库和文件。更新期间服务可能短暂中断，备份保留在仅 root 可读的 `/var/lib/starhub-updater/`。Windows、Compose 或助手未就绪时页面明确提示手动升级。只允许主管理员操作，委派 API 密钥无更新权限。

自动更新不切换宝塔 Nginx、代理内核或其他 VPS 的 Agent。已有 `/opt/harbor` 由本版升级包安装助手；其他受支持 Linux 安装的启用方式和兼容要求见 [发布说明](docs/RELEASE-v0238.md)。

## 查询面板地址与临时 IP 备用入口

以下 Linux 命令以 **root** 执行；已经是 `root@主机名:~#` 时不需要 `sudo`。普通用户可先 `su -`，或使用已配置的 `sudo`。一键新装目录是 `/opt/starhub-control`，旧部署的 `/opt/harbor` 是另一种布局，不能混用路径。

查询面板地址和临时恢复入口状态（不显示登录令牌）：

```bash
python3 /opt/starhub-control/panel-access.py status --env /opt/starhub-control/control.env
```

域名故障时，临时开启 **IP:18081，30 分钟后自动关闭**：

```bash
python3 /opt/starhub-control/panel-access.py enable-ip --env /opt/starhub-control/control.env --allow-http --minutes 30
```

手动关闭临时恢复入口：

```bash
python3 /opt/starhub-control/panel-access.py disable-ip --env /opt/starhub-control/control.env
```

**18080 与 18081 是两个入口：** 一键安装的 `--public` 默认公开普通网页 `IP:18080`；`enable-ip` 额外创建临时 `IP:18081`。因此 `disable-ip` 只停止临时恢复服务，不会关闭普通的 18080、域名网关、Agent 或数据库；`recovery.active=false` 也不代表普通 IP 入口已关闭。普通网页监听以 `status` 返回的 `web_listener` 为准。

临时入口使用未加密 HTTP，需显式加 `--allow-http`；脚本不会自动放开主机 / 云防火墙。明确需要常开可加 `--persistent`。也可在自己的电脑通过 SSH 转发，无需开放备用公网端口：

```bash
ssh -N -L 18082:127.0.0.1:18080 YOUR_USER@YOUR_VPS
# 本机浏览器打开 http://127.0.0.1:18082
```

上面的转发目标对应新装默认端口；自定义端口或旧部署请按实际监听配置调整。旧布局完整命令、脚本缺失时的自动 root / sudo 下载查询、自定义目录、Windows 与常见报错见 [面板访问命令](docs/PANEL-ACCESS.md)。Compose 使用自己的端口映射和 SSH 转发，见 [安装文档](docs/INSTALL.md)。

### 永久关闭公网面板端口，保留域名访问

一键新装 Linux systemd 可用下面的**完整单行命令**。以 root 执行；自动从本机已保存的站点配置读取域名和网页端口，无须填写域名占位符。直接复制整行，不插入反斜杠续行。

```bash
( set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/panel-access.py -o "$f"; python3 "$f" disable-public --env /opt/starhub-control/control.env )
```

脚本校验 HTTPS 域名的本机回源及面板版本，再备份配置，将普通网页改为回环监听，重启并复查域名，最后关闭临时恢复服务；失败恢复普通网页配置。更新 / 回滚进行中会拒绝冲突操作。结果中的 `backup` 是私有备份路径，`web_listener` 是目标监听。默认新装关闭公网 18080，域名网关仍在 80/443 工作；普通后台更新和重启保留该配置。

```bash
ss -lntp | grep -E ':18080|:18081'
```

预期只看到 `127.0.0.1:18080`。不要再粘贴旧示例中的 `YOUR_PANEL_DOMAIN`；反斜杠后若有空行，会把 curl 截断成“no URL specified”。关闭端口不能隐藏源站 IP。详细边界、备份和恢复见 [永久关闭普通网页公网监听](docs/PANEL-ACCESS.md#永久关闭普通网页公网监听)。

## Linux 一键卸载

默认只卸载本项目运行组件，保留数据库、私有配置和备份。去掉 `--apply` 可以预览。彻底删除数据需自行备份后显式添加 `--purge-data`；远程 Agent 先在主机页逐台退役。以下命令使用 root，不需要 sudo。

一键新装的本地卸载命令：

```bash
python3 /opt/starhub-control/uninstall.py --layout quickstart --prefix /opt/starhub-control --apply
```

仅旧 `/opt/harbor` 布局使用：

```bash
python3 /opt/harbor/uninstall.py --layout legacy --apply
```

本地脚本缺失时的下载命令、普通用户及自定义目录用法见 [安装文档](docs/INSTALL.md#卸载面板运行组件)。

## 域名一直等待签发 / 新装网关修复

IP + 端口能访问只代表控制面已启动。Linux systemd 新装会安装 Nginx、Certbot、Cloudflare DNS 插件和受控网关助手；已有宝塔 / Nginx 会优先识别并保留原网站。旧版安装若只有控制面和更新服务，可使用 [网关安装与修复命令](docs/GATEWAY.md)，无需重装面板或重建令牌。

## Linux 获取面板管理员登录令牌

仅显示这台星枢面板已有的 `ADMIN_TOKEN`，不会生成、轮换或重置令牌；与宝塔账号无关。将输出复制到星枢登录页，不要粘贴到聊天、日志或 GitHub。

**已经是 root（例如终端提示 `root@主机名:~#`）时，直接运行 `python3`，不需要 `sudo`，也不必为此安装 `sudo`。** 普通用户可使用已配置的 `sudo`；如果没有，先执行 `su -` 切换到 root。

一键新安装 Linux 的默认目录是 `/opt/starhub-control`。root 用户直接执行：

```bash
python3 /opt/starhub-control/admin-token.py --env /opt/starhub-control/control.env
```

普通用户（系统已安装且允许使用 `sudo`）：

```bash
sudo python3 /opt/starhub-control/admin-token.py --env /opt/starhub-control/control.env
```

如果已经按旧文档将脚本下载到 `/tmp/starhub-admin-token.py`，root 用户无需重新下载：

```bash
python3 /tmp/starhub-admin-token.py --env /opt/starhub-control/control.env
```

通用下载命令：自动区分 root / 普通用户，并自动识别默认的新旧安装配置；没有 `sudo` 的普通用户会得到明确提示。

```bash
(
  set -eu
  if [ "$(id -u)" -eq 0 ]; then
    token_reader=python3
  elif command -v sudo >/dev/null 2>&1; then
    token_reader='sudo python3'
  else
    printf '%s\n' '当前不是 root 且未安装 sudo，请先执行 su - 切换到 root，再运行本命令。' >&2
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

旧 `/opt/harbor` 部署布局的 root 命令（普通用户在 `python3` 前加 `sudo`）：

```bash
python3 /opt/harbor/admin-token.py --env /etc/harbor/control.env
```

源码 / Compose / 自定义 `--prefix` 安装：把下面两个占位路径替换成实际的**绝对路径**。不要在 `/root` 直接运行 `python3 scripts/admin-token.py`；该相对路径仅在源码项目目录中有效。

```bash
python3 /ABSOLUTE/PROJECT/scripts/admin-token.py --env /ABSOLUTE/PRIVATE/.env
```

`sudo: command not found` 表示系统没有安装 `sudo`，root 用户去掉它即可；`can't open file '/root/scripts/admin-token.py'` 表示当前目录下没有该源码脚本。若提示无法唯一定位配置，使用 `--env` 指定这台面板实际的 `control.env` 或私有 `.env`，不要新建令牌。

## Windows 一键卸载

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -OutFile "$env:TEMP/starhub-uninstall.py"; python "$env:TEMP/starhub-uninstall.py" --apply
```

## Windows 获取面板管理员登录令牌

```powershell
Invoke-WebRequest https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/admin-token.py -OutFile "$env:TEMP/starhub-admin-token.py"; python "$env:TEMP/starhub-admin-token.py"
```

Nginx / 宝塔站点、Docker 本身及其他软件不会随面板卸载。更多私有配置路径、备份与恢复见 [安装文档](docs/INSTALL.md)。

## 0.13 新增与修正

- 协议/传输/安全分层，按内核联动校验；新增 HTTP/SOCKS/Mixed/Hysteria v1/Naive 及 HTTPUpgrade/XHTTP/HTTP2/mKCP 适配，完整范围见工作流。
- Telegram 加密绑定、只读命令授权、定时报告内容选择与事件提醒，默认关闭。
- 防火墙版本编辑、回收站删除/恢复、本机受控移除和取消，保留独立回滚。
- 原生配置识别草稿、客户端路由引用检查、可视化规则自动同步、嵌套组展开去重。
- 空授权账号不再可能变为免认证；新增端口失败时保留完全未变的有效入站。补齐扩展协议的用户流量与访问日志解析。
- 本版控制面与新安装 Agent 为 **0.25.6**；主机流量单位设置只需升级控制面，现有 0.23.17 Agent 可继续使用。
- [0.13 功能、配置、通知与兼容边界](docs/WORKFLOW-v013.md)。

## 0.12 新增

- 区分节点发布地址与 VPS 出站类型；IPv4 / IPv6 多选，默认使用所选主机 IP，无 IPv6 时禁选；双地址复用同一个入站进程。
- 用户组统一授权 SS / Snell 与独立用户协议。共享密钥按入口计量，无法伪装成按用户限额或独立撤销。
- 用户 + 客户端分流策略，Mihomo YAML / 可视化编辑、稳定节点引用、按协议与 IP 类型动态匹配，自动转 sing-box / Xray。
- Hiddify、NekoBox、v2rayNG、v2rayN、Clash Verge 等客户端选择；完整配置携带规则，URI 仅节点，显式筛选兼容协议。
- 每月指定日重置（例如 5 日 → 下月 5 日），月末缺日取最后一天，后续保持原设定日。
- 可选后台域名、IP + 端口、独立订阅域名；默认订阅入口按订阅域名 → 主站域名 → IP 回退。
- Linux Agent 0.12 受控远程卸载：等待真实回执后清理控制端数据；不会卸载控制面、数据库或其他软件。
- [本版工作流与限制](docs/WORKFLOW-v012.md)。

## 0.11 新增

- 协议多选 / 全选兼容内核协议，同主机原子保存，端口自动分配，重试不重复创建。
- 按设置日起每满一月重置，支持自定义周期起点；Clash/Mihomo 原生 YAML 与完整用量响应头。
- VPS 协议参数 JSON 编辑，客户端 JSON/YAML 模板编辑、订阅绑定用户修改和用量诊断。
- 星枢品牌与可修改站点名；按功能授权 API 密钥、有效期、限流、撤销及独立前端 CORS。
- [API 与集成说明](docs/API-INTEGRATIONS.md) · [OpenAPI](docs/openapi.json)。出站策略与受控卸载需要升级 Agent 0.12；已有协议可继续运行。

## 本版功能

- 主机编辑、停用后删除、单次 15 分钟 Agent 接入命令；Agent 主动连出，无需开放目标设备管理端口。
- 每台主机每种内核合并运行一个进程；VLESS、VMess、Trojan、Hysteria2、TUIC、AnyTLS、Snell、Shadowsocks 模板，含入站编辑、启停和确认停止后删除。
- 独立用户身份、用户组、到期、流量额度、每日 / 每月 / 手动重置、额度耗尽撤销；共享密码入口按入口统计。
- sing-box、Xray JSON、Mihomo、Shadowrocket 显式选择；客户端配置模板、链接重新复制、轮换和撤销。客户端能力不支持时明确拒绝导出。
- IPv4 / IPv6、FRP TCP 穿透、至多 8 跳链式代理；域名 HTTPS、免费 ACME 证书、受限反代网关、防火墙预览和本地受限应用工具。
- 明暗 / 系统主题、移动端布局、按需加载；连接元数据保留 7 天 / 20,000 条，不记录 HTTPS 页面内容。

## 本地启动

需要 Docker Engine + Compose v2（Windows 使用 Docker Desktop Linux 容器模式）和 Python 3。

```bash
bash scripts/install.sh compose
```

```powershell
.\scripts\install.ps1 -Mode compose
```

打开 http://127.0.0.1:5173，使用本机 `.env` 的 `ADMIN_TOKEN`。安装器生成缺失随机凭据并保留现有 `MASTER_KEY`。API、分享端、前端仅绑定回环；数据库不发布公网端口。远程管理经受限 HTTPS 网关或 SSH 转发，详见 [安装](docs/INSTALL.md)。

```bash
ssh -N -L 5173:127.0.0.1:5173 YOUR_USER@YOUR_VPS
```

## 手动构建与测试

源码构建使用 Go **1.27.3+**、Node.js 22.12+、Python 3；原生控制面需要 PostgreSQL 17。构建完成后无需 Node 常驻。

```bash
bash scripts/install.sh build
python scripts/prepare.py
# 在私有 .env 配置 DATABASE_URL 和 WEB_DIR（web/dist 绝对路径）
bash scripts/run-native.sh control
```

Windows 使用 `./scripts/install.ps1 -Mode build` 和 `./scripts/run-native.ps1 -Role control`。Linux / Windows amd64 / arm64 的四平台构建见 GitHub Build 工作流；目标机安装细节见 [INSTALL.md](docs/INSTALL.md)。

```bash
go vet ./...
# 集成测试只使用专用测试库，自动创建 / 清理隔离 schema
export TEST_DATABASE_URL='postgres://TEST_USER:TEST_PASSWORD@127.0.0.1:5432/harbor_test?sslmode=disable'
go test -count=1 ./...
python scripts/test_domain_sync.py
python scripts/test_managed_gateway.py
python scripts/test_agent_bootstrap.py
cd web && npm ci && npm test && npm run build
```

## 内核与一键接入包

安装器采用固定下载地址和 SHA-256 校验。本版使用官方 sing-box 1.14.4、Xray 26.10.21、FRP 0.71.1 发行文件。下载时核对官方资产 SHA-256，不再使用本项目定制后缀。主机卡片的“内核版本”可以查询最新稳定版、安装或切换指定版本；Agent 先检查当前配置，启动失败时回退。

```bash
python scripts/fetch-engines.py
python scripts/build-agent-releases.py --output agent-releases-v010
```

fetch-engines.py 下载官方发行文件，无需 Go。使用 --sing-box-version / --xray-version / --frp-version 指定标签或 latest；脚本不会覆盖已有 engines/。Agent 包附带未修改的官方 sing-box 对应源码与许可。官方来源及摘要校验不等同于没有上游漏洞，升级后仍需关注官方安全公告。`AGENT_RELEASE_DIR` 指向新版本目录后，面板可生成目标 VPS 的接入命令，不覆盖已安装的 Agent。

## 使用与边界

在系统设置修改域名 / HTTPS，网关实际状态另行回报。普通 TLS 需要自己域名对应的证书；**只有 REALITY 的目标与 SNI 可以选大厂候选站点**，仍需从目标 VPS 验证可达性。证书不能用任意大厂域名替代。VMess AEAD 本身不强制证书，是否需要证书取决于外层 TLS。

流量额度用于 6 种具有独立用户认证的模板；Snell / 当前 Shadowsocks 模板使用共享密钥，只能按入口计量。统计约 10 秒上报，短时超额和异常退出时少量计量误差仍可能出现。流量从升级后开始，历史无法补算。详见 [计量](docs/TRAFFIC-v010.md)。

无公网 IPv4 设备可主动连接可达中继；IPv6-only 入口要求客户端具备 IPv6 可达性。系统仍是单管理员个人平台，尚无商业多租户隔离、MFA / OIDC、支付、生产恢复演练或完整负载测试。安全扫描不等于不存在漏洞，见 [安全检查](docs/SECURITY-v010.md)。

订阅 URL 撤销不会收回已下载的代理凭据；需要撤销用户、轮换用户凭据或停用对应入口。数据库和 `MASTER_KEY` 必须分别备份，密钥不能提交 GitHub。

## 文档

- [本版功能与待办](docs/FEATURES.md)
- [计量、配额与访问记录](docs/TRAFFIC-v010.md)
- [协议 / CRUD 工作流](docs/WORKFLOW-v09.md)
- [安全检查与内核构建](docs/SECURITY-v010.md)
- [主机接入](docs/AGENT-CONNECT.md) · [域名 / NAT / IPv6](docs/PUBLISH.md)
- [防火墙](docs/FIREWALL.md) · [OpenAPI](docs/openapi.json)
- [后续路线](docs/ROADMAP.md)


## 卸载与管理员令牌

Linux / Windows 通用命令见本文前部，更多布局见 [安装文档](docs/INSTALL.md)。令牌是星枢面板的 ADMIN_TOKEN。

## 控制端换域名与 Agent 地址修复

「系统设置 → 站点与域名」可单独设置最多 4 个 Agent HTTPS 控制地址（按优先级）。地址必须事先拥有有效证书，并反代至同一个管理 API；该输入框不自动创建第三个 Nginx 网关。留空跟随控制台域名，订阅域名单独维护。

先保持旧域名可达，设置并验证新地址。Agent **0.23.17+** 从已鉴权心跳获取地址，通过绑定目标域名和随机挑战的 HMAC 证明后切换；不跟随 HTTP 重定向、不绕过 TLS、不向未经验证的目标发送令牌。已验证地址保存在私有状态目录，重启后可恢复。变更时请保留旧入口，等全部主机的“控制地址”回报更新后再关闭。

若原地址已失效，进入「连接主机 → 连接 / 恢复 → 生成控制端地址修复命令」，在原 VPS SSH 执行。保留 HOST_ID / AGENT_TOKEN、内核、协议配置和流量账本，无需删除主机或重新注册。“生成 Agent 升级命令”会校验本控制端版本对应的 GitHub 正式发行包，仅更新标准 Linux `/opt/harbor-agent` 安装的 Agent；失败恢复旧二进制，现有内核不切换。总管理机的旧布局 Agent 由面板升级流程维护。两种操作均可能短暂中断本机托管代理。

## 数据库内存

内存上限不是预占内存。新安装和 Compose 默认 512 MiB，Compose 用 `POSTGRES_MEMORY_LIMIT`，新安装用 `quickstart.py --db-memory-mb 512` 调整。旧 Linux 安装的更新助手仅扩大过小上限：物理内存 ≥1.5 GiB 时为 512 MiB，≥768 MiB 时为 384 MiB，否则为 256 MiB；保持原有更大或不限额设置，不重启数据库、不清理数据、不自动调高连接数。设置 → 后台版本可查看最近回报的限制与主机内存。

需要更大容量时，在一键新装的 SSH 中以 root 执行（只允许扩大上限，不需要 sudo）：

```bash
python3 /opt/starhub-control/scripts/panel-updater.py --env /opt/starhub-control/control.env --database-memory-mb 768
```

旧布局改用 `/opt/harbor/scripts/panel-updater.py --env /etc/harbor/control.env`。几十台 VPS / 上百用户不是单靠内存额度就能保证的容量：持续观察峰值、OOM、响应延迟、磁盘、计量断报与备份；默认应用连接池 4 条并复用连接，VPS 数不等于数据库连接数。

端口跳跃：Hysteria / HY2 的可选设置、Linux 受限助手和客户端格式说明见 [PORT-HOPPING.md](docs/PORT-HOPPING.md)。现有节点默认关闭，远端主机需要先执行面板提供的助手安装 / 更新命令。

HY2 服务端带宽与客户端带宽可分别设置；TUIC / AnyTLS 专属客户端参数也支持完整订阅同步。用法、单位、方向、协议范围及兼容限制见 [协议参数与性能设置](docs/PROTOCOL-PERFORMANCE.md)。
