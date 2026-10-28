# 星枢 · VPS 多机管理 0.21.0

订阅节点的 IPv4 / 域名入口保留后台原名，IPv6 使用「原名-v6」。例如 `snell_pro` / `snell_pro-v6`，不附加随机 ID；策略仍按稳定端点引用选择，改名不会改变授权。重名、与策略组或客户端保留名称冲突时明确提示修改，不生成无法导入的配置。[本版说明](docs/WORKFLOW-v021.md)。

主流程：**连接主机 → 配置内核与协议 → 用户 / 用户组权限 → 客户端策略 → 用户原订阅分享**。使用 Clash Verge / FlClash 的设备名额时选择兼容模式；该模式统计订阅访问，不证明真实物理设备。已有链接自动跟随用户组授权，无须重建用户。

## 一键部署新总管理端

脚本检测 Python >=3.9、HTTPS 下载工具与 Docker Linux 容器引擎，缺少时自动安装，已有可用环境直接复用。预编译包按架构选择并核验固定 SHA-256，不需要 Go / Node。已有安装、数据卷、服务或端口冲突会停止，不能用此命令覆盖升级旧面板。

Linux（Debian 11+ / Ubuntu 22.04+、Fedora、Rocky / Alma / RHEL 8+ / CentOS Stream、openSUSE / SLES 的 systemd 系统；需 root 或 sudo）：

```bash
sh -c 'set -eu; S=; if [ "$(id -u)" != 0 ]; then command -v sudo >/dev/null || { echo "Run as root"; exit 1; }; S=sudo; fi; if ! command -v curl >/dev/null; then if command -v apt-get >/dev/null; then $S apt-get update; $S apt-get install -y curl ca-certificates; elif command -v dnf >/dev/null; then $S dnf install -y curl ca-certificates; elif command -v zypper >/dev/null; then $S zypper --non-interactive install curl ca-certificates; elif command -v apk >/dev/null; then $S apk add --no-cache curl ca-certificates; else echo "Install curl first on this OS"; exit 1; fi; fi; F=$(mktemp); curl --proto "=https" --tlsv1.2 -fL https://github.com/czy-code-923/czy-code/releases/download/v0.21.0-distribution/install-starhub.sh -o "$F"; $S sh "$F" --public; rm -f "$F"'
```

Windows 10/11 **管理员 PowerShell**：自动寻找或安装 Python、WinGet、Docker Desktop / WSL 组件，启动并等待 Linux 引擎。首次启用 WSL 需要重启时会提示，重启后重跑同一命令；不会自动重启、切换已有 Windows 容器或跳过 Docker 首次许可 / 虚拟化要求。Windows Server 使用 Linux 虚拟机。

```powershell
Invoke-WebRequest https://github.com/czy-code-923/czy-code/releases/download/v0.21.0-distribution/install-starhub.ps1 -OutFile "$env:TEMP/install-starhub.ps1"; & "$env:TEMP/install-starhub.ps1" -Public
```

默认公开 IP:18080 的认证网页（HTTP）；安装后可在设置绑定域名和 HTTPS。内部 API、分享处理器及数据库仍只监听回环。若要仅本机访问，去掉 `--public` / `-Public`。Windows 默认当前用户登录任务；Linux 服务由 systemd 管理。Alpine / OpenRC 可自动准备依赖，但面板用包内 `quickstart.py --foreground` 配合自己的监督器。环境安装不会卸载旧 Docker、清理镜像/数据或放开数据库端口。

[完整源码与四平台附件](https://github.com/czy-code-923/czy-code/releases/tag/v0.21.0-distribution) · [手动 / Compose / 离线安装](docs/INSTALL.md)

## Linux 一键卸载

默认只卸载本项目运行组件，保留数据库、私有配置和备份。去掉 `--apply` 可以预览。彻底删除数据需自行备份后显式添加 `--purge-data`；远程 Agent 先在主机页逐台退役。

```bash
curl -fL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/uninstall.py -o /tmp/starhub-uninstall.py && sudo python3 /tmp/starhub-uninstall.py --apply
```

已在当前管理 VPS 安装的本地卸载命令：

```bash
sudo python3 /opt/harbor/uninstall.py --apply
```

## Linux 获取面板管理员登录令牌

仅显示已有星枢面板 `ADMIN_TOKEN`，不会生成或重置；与宝塔账号无关。不要把输出粘贴到聊天、日志或 GitHub。

```bash
curl -fL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/admin-token.py -o /tmp/starhub-admin-token.py && sudo python3 /tmp/starhub-admin-token.py
```

当前管理 VPS 的本地命令：

```bash
sudo python3 /opt/harbor/admin-token.py
```

源码 / Compose 安装，在项目目录读取自己的私有配置：

```bash
python3 scripts/admin-token.py --env .env
```

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
- 控制面与 Agent 均为 **0.21.0**；新功能需升级 Agent。
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
