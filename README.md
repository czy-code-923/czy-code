# Harbor · VPS 管理平台 v0.5

Vue 3 / TypeScript / Go / PostgreSQL 个人控制面：库存、模拟/暂存任务、防火墙策略，以及显式启用的真实 sing-box、跨机 FRP、中转和域名订阅分享。

**基础控制面已部署到真实 VPS，HTTPS 页面可访问。** 运行目录、登录方式、资源限制、验收与未完成项见 [实际部署记录](docs/DEPLOYMENT-v04.md)。本地模拟通过与真实代理上线是不同状态。

## 连接其他 VPS

这台是总管理端。进入「主机资产 → 新建主机」只填名称，复制自动生成的安装命令，在目标 VPS 执行即可接入。无需填写 HOST_ID/长效令牌、写 Compose 或安装数据库。支持 Linux amd64/arm64 + systemd；命令15分钟有效、单次使用，连接状态依据真实心跳。详见 [一键连接与保护机制](docs/AGENT-CONNECT.md)。

## 一键启动

前提：Docker Engine + Compose v2（Linux）或 Docker Desktop Linux 容器模式（Windows），以及 Python 3。在解压后的源码根目录运行：

```bash
bash scripts/install.sh compose
```

```powershell
.\scripts\install.ps1 -Mode compose
```

打开 <http://127.0.0.1:5173>，用 `.env` 的 `ADMIN_TOKEN` 登录。脚本生成缺失的随机凭据，保留旧密码及 MASTER_KEY。管理端口只映射回环，数据库不发布端口；不要把8080、5173、8091映射到公网。远程管理使用 SSH 转发：

```bash
ssh -N -L 5173:127.0.0.1:5173 YOUR_USER@YOUR_VPS
```

## 域名、服务和分享

1. 在「域名与分享」设置域名、选择HTTPS；保存是期望设置，界面另列网关实际回报。
2. 按 [安装文档](docs/INSTALL.md) 安装本地受限域名同步器。它维护自己的nginx站点，处理ACME证书、校验配置后重载，失败恢复。启用计时器后面板更换域名自动同步，DNS需提前配置。
3. Linux 目标主机优先使用面板一键连接，安装器配置真实服务执行器；Windows 保留独立 Agent 手动登记方式。
4. 创建Shadowsocks 2022（IPv4/IPv6/域名、最多8跳）或FRP服务端/客户端。默认停用、默认回环监听；核对防火墙及端口后启用。
5. 选择服务创建有期限订阅，支持sing-box、Mihomo/Clash Meta、Base64 URI列表；链接仅显示一次，可轮换和撤销。只有新鲜回报运行的服务会下发。

无公网IPv4设备的Agent和FRP客户端主动连出，不需要公网管理端口；两端都没有可达入口仍需要中继。IPv6-only入口要求客户端具备IPv6可达性，域名不会自动消除这一限制。

撤销URL不会撤销已导入的代理密码；泄露后需停用并创建替代服务。MASTER_KEY用于加密真实服务凭据，必须和数据库分别安全备份。

## 文档

- [Linux/Windows、Compose/原生/离线、Agent、宝塔网关](docs/INSTALL.md)
- [域名、订阅、NAT与IPv6](docs/PUBLISH.md)
- [完整功能检查与边界](docs/FEATURES.md)
- [v0.4验证记录](docs/VERIFICATION-v04.md)
- [防火墙与防锁死](docs/FIREWALL.md)
- [资源优化](docs/PERFORMANCE.md)
- [OpenAPI](docs/openapi.json)
- [后续路线](docs/ROADMAP.md)

## 原生手动构建

需要Go 1.25+、Node.js 22.12+、PostgreSQL 17、Python 3：

```bash
bash scripts/install.sh build
python scripts/prepare.py
# 在私有.env加入自己的DATABASE_URL与WEB_DIR（web/dist绝对路径）
bash scripts/run-native.sh control
```

Windows使用 `./scripts/install.ps1 -Mode build` 和 `./scripts/run-native.ps1 -Role control`。原生控制面自带回环静态Web服务，不需要Node常驻。手动触发GitHub的Build工作流可生成Linux/Windows amd64/arm64二进制与前端产物；产物包使用原生运行，Compose从完整源码构建。

```bash
go vet ./...
go test -count=1 ./...
# 专用测试库，集成测试自动创建/清理隔离schema；不要使用生产库
export TEST_DATABASE_URL='postgres://TEST_USER:TEST_PASSWORD@127.0.0.1:5432/harbor_test?sslmode=disable'
go test -count=1 ./...
python scripts/test_domain_sync.py
cd web
npm ci
npm run build
npm test
```

原生转发测试另需 TEST_SING_BOX_BINARY / TEST_FRPS_BINARY / TEST_FRPC_BINARY 指向官方引擎，缺失时明确跳过。

## 范围与安全

当前是个人管理员版本，预留租户字段不等于完整多租户；没有计费、自助开通、MFA或RBAC。API/Agent没有任意shell、脚本上传或任意原生配置入口，受管引擎由固定参数和本地SHA-256约束。Agent普通用户运行，Linux防火墙和域名网关由独立、管理员安装的本地工具处理。

库存「中转规划」仍是规划表，真实中转通过「真实服务」下一跳执行；历史模拟/暂存任务与真实服务状态分开。防火墙保存不等于应用，仅管理自有Linux INPUT表，不接管宝塔、Docker NAT、云安全组或Windows Defender Firewall。

协议依据：[sing-box Shadowsocks](https://sing-box.sagernet.org/configuration/inbound/shadowsocks/)、[FRP TLS验证](https://gofrp.org/en/docs/features/common/network/network-tls/)、[SIP002](https://shadowsocks.org/doc/sip002.html)。官方引擎不随源码分发，下载与使用遵守其各自许可证。
