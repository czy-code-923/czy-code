<!-- 0.14 workspace update -->

0.14 新增四分区客户端工作区：基础、DNS、策略组与规则。默认模板基于提供的 Mihomo Max（21 组 / 186 规则），无服务器凭据。支持配置识别、节点映射、单项编辑、客户端转换预览和 Shadowrocket 配套节点/分流链接。具体转换差异见 [0.14 使用说明](docs/WORKFLOW-v014.md)。

# 星枢 · VPS 多机管理 0.15.0

0.15 修复 Telegram 命令接收与 VPS 时钟偏差，发送和接收状态分开显示。机器人查询须启用长轮询并授权用户 ID；私聊可在通知设置中填入当前绑定用户后保存。策略页统一管理客户端配置，等宽列表独立滚动；订阅页只管理分享，历史输出覆盖在策略页高级区域保留。首页显示有限余额、完整日期趋势与用户范围统计，访问记录按需加载。详见 [本版行为与边界](docs/WORKFLOW-v015.md)。


Vue 3 / TypeScript / Go / PostgreSQL 的个人 VPS 控制面。主流程：**连接主机 → 配置内核与协议入站 → 分配用户 / 用户组 → 客户端策略与规则 → 分享客户端订阅**。首页显示真实流量、额度、主机状态及连接元数据。

本项目已在授权的管理 VPS 部署；新建服务默认停用。演示、配置校验通过与真实内核已运行分别展示。旧版本记录留在 docs/，本版以 [功能检查](docs/FEATURES.md)、[0.10 计量说明](docs/TRAFFIC-v010.md) 为准。

## 一键部署新总管理端

前提：Python 3、可用的 Docker（Windows 为 Docker Desktop Linux 容器模式）。脚本下载与 CPU 架构匹配的预编译包并校验固定 SHA-256；无需本机 Go / Node。已有安装、同名服务或端口冲突会停止。

Linux（Debian / Ubuntu / Rocky / Alma / Fedora / openSUSE 等 systemd 系统）：

```bash
curl -fL https://github.com/czy-code-923/czy-code/releases/download/v0.15.0-distribution/install-starhub.sh -o /tmp/install-starhub.sh && sudo sh /tmp/install-starhub.sh --public
```

Windows PowerShell：

```powershell
Invoke-WebRequest https://github.com/czy-code-923/czy-code/releases/download/v0.15.0-distribution/install-starhub.ps1 -OutFile "$env:TEMP/install-starhub.ps1"; & "$env:TEMP/install-starhub.ps1" -Public
```

`--public` / `-Public` 明确启用 IP:18080 的认证网页入口；HTTP 不加密，建议设置 HTTPS 网关。省略此参数则只监听回环。内部 8080 API、8091 分享处理器和 PostgreSQL 均不暴露公网。管理员令牌保存在本机私有 control.env，不打印、不上传。Windows 安装当前用户登录任务，退出登录后不保证运行；Alpine/OpenRC 用包内 `python3 scripts/quickstart.py --foreground` 配置自己的进程监督器。依赖安装与 HTTPS 网关、离线、源码、Compose 方式见 [安装文档](docs/INSTALL.md)。

完整源码请下载 Release 附件 **harbor-0.15.0-source.zip**；GitHub 自动生成的 Source code 归档对应标签所指提交，可能早于分发附件。

## 0.13 新增与修正

- 协议/传输/安全分层，按内核联动校验；新增 HTTP/SOCKS/Mixed/Hysteria v1/Naive 及 HTTPUpgrade/XHTTP/HTTP2/mKCP 适配，完整范围见工作流。
- Telegram 加密绑定、只读命令授权、定时报告内容选择与事件提醒，默认关闭。
- 防火墙版本编辑、回收站删除/恢复、本机受控移除和取消，保留独立回滚。
- 原生配置识别草稿、客户端路由引用检查、可视化规则自动同步、嵌套组展开去重。
- 空授权账号不再可能变为免认证；新增端口失败时保留完全未变的有效入站。补齐扩展协议的用户流量与访问日志解析。
- 控制面与 Agent 均为 **0.15.0**；新功能需升级 Agent。
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

安装器采用固定下载地址和 SHA-256 校验。当前服务端使用 **sing-box 1.14.4-harbor.1**（官方 1.14.4 源码、修复依赖、Go 1.27.3）、Xray 26.10.21、FRP 0.71.1。修复版是本项目构建，不能冒充官方二进制。

```bash
python scripts/fetch-engines.py
python scripts/build-agent-releases.py --output agent-releases-v010
```

源码包的 fetch-engines.py 自动用 Go 构建修复版；预编译包自带 patched-engines，校验后直接使用，无需 Go。脚本不会覆盖现有 engines/，升级应使用新目录。构建脚本提供完整对应源码 ZIP、GPL 许可和构建回执；Agent 包附带该源码与许可。`AGENT_RELEASE_DIR` 指向新版本目录后，面板可生成目标 VPS 的接入命令，不覆盖已安装的 Agent。

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
