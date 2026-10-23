# v0.8 主机、内核、入站与用户授权

## 实际部署关系

`物理主机 → 已校验内核 → 完整部署版本（多个入站 + 各自出站 + 入站路由）→ 逻辑节点 → 用户组授权 → 用户订阅`

主机在线仅表示 Agent 心跳正常。Agent 0.8 会检查固定二进制 SHA-256、执行版本查询并回报各内核就绪状态；启用服务要求该内核近期已回报可用。创建未启用配置可以先于主机上线完成。

控制端仅选取当前认证主机的配置，将同一主机的 sing-box 入站合并为一个 sing-box 配置、Xray 入站合并为另一个 Xray 配置。每个内核只运行一个进程，不再给每个入站单独启动内核。不同主机可运行完全不同的协议和端口。FRP 服务端与客户端保留独立生命周期。

每个入站有唯一 tag，对应独立出站 tag；路由明确匹配入站，不依赖数组顺序。证书文件名按入站隔离。加密保存主机内核快照，最多保留 20 个；面板仅提供隐藏凭据的完整配置预览。Agent 原生检查成功后才切换配置，通过进程及配置摘要回报运行状态。拒绝任意 shell、任意文件路径和任意原生 JSON 注入。

同一内核配置必须整体有效。新版用户授权配置或原生检查失败时停止该内核，避免继续接受已撤销身份；其他内核和 FRP 不受影响。配置变更及撤销将在下一次 10 秒同步后生效；控制端持续失联 90 秒时停止真实服务。重启内核会中断现有连接。

旧 Agent 继续支持旧模板；REALITY、gRPC 和用户授权要求 Agent 0.8。旧版本不能接收这些启用配置，以免忽略新字段。新主机仍从面板生成一次性、一键接入命令，安装包包含固定内核。

## 协议与安全层

| 协议 | 内核 | 必要身份 | 外层安全 | 已管理可选项 |
|---|---|---|---|---|
| VLESS | sing-box / Xray | UUID | TLS 或 TCP REALITY | Vision、WS 路径与 Host、gRPC 服务名、ALPN、用户组 |
| VMess AEAD | sing-box / Xray | UUID | TLS 可选；VMess 本身不要求证书 | TCP / WS / gRPC、Host、ALPN、用户组 |
| Trojan | sing-box / Xray | 密码 | TLS | TCP / WS / gRPC、Host、ALPN、用户组 |
| Hysteria2 | sing-box | 密码 | QUIC + TLS | ALPN、用户组 |
| TUIC v5 | sing-box | UUID + 密码 | QUIC + TLS | cubic / bbr / new_reno，固定 h3 ALPN，用户组 |
| AnyTLS | sing-box | 密码 | TLS | ALPN、用户组 |
| Shadowsocks 2022 | sing-box | 32 字节 Base64 密钥 | 协议自身加密 | 固定 AES-256-GCM 密码套件，独立共享密钥入站 |
| Snell | sing-box | 密码 | 协议自身加密 | v5 服务端的 v4 兼容客户端，独立共享密钥入站 |

TLS：持有与 SNI 匹配且公开受信任的完整证书链和私钥，可选择现有 ACME 自动续期证书或手动证书。任意改填大厂 SNI 不会绕过证书校验。

REALITY：选择目标域名与端口、目标接受的 SNI，自动生成 X25519 密钥和 Short ID。可以使用适合的第三方目标站，无需其域名控制权或 CA 证书。目标站需支持合适 TLS 1.3 握手，并须从具体 VPS 检查可达性；界面不会把示例站点标为自动验证通过。私钥仅发给实际承载该入站的主机；客户端和链路前一跳仅接收公钥。

### 已发现的真实版本兼容边界

固定版本为 sing-box 1.14.3、Xray 26.10.21、FRP 0.71.1。Xray 的 REALITY 服务端要求 X25519MLKEM768 ClientHello，而 sing-box 1.14.3 REALITY 客户端源码会移除该扩展。已通过转发测试的组合：sing-box REALITY → sing-box 客户端；Xray REALITY → Xray 客户端。Xray REALITY 对 sing-box、Mihomo、Shadowrocket 导出返回 422，并拒绝以 sing-box 作为此节点的链路前一跳，避免生成已知不兼容配置。后两种客户端的具体版本未完成验证前不宣称兼容。

原生内核支持目录包含更多协议，但目录中的“内核支持”不代表面板已完成受管表单和实际互通测试。XHTTP、VPN/TUN、WARP、全套原生字段和高级流量策略仍需单独实现和验证，不能通过一个通用 JSON 编辑器假装支持。

## 用户、用户组和分享

代理用户不是管理员账号。每个用户独立生成 UUID、密码并加密保存；管理员仅维护名称、启停、到期时间和凭据轮换，不回显密码。入站开启按用户授权后，仅把有效组成员编入服务端 users 数组，没有授权用户时不使用默认共享凭据回退。下一跳使用独立保留的机器身份，仅有链路引用时才加入服务端配置。

用户组关联用户与入站；多组权限取并集、去重。组变更和用户更新自动重编主机配置。用户到期在同步时剔除，无需等待订阅再次下载。独立共享密钥的 SS 2022 / Snell 暂不加入多用户组；可使用独立入站及单独订阅，不提供虚假的逐用户撤销。

用户专属订阅动态跟随组权限，使用用户自己的凭据，不分享服务端默认身份。停用用户阻止下载并撤销服务器认证；撤销订阅 URL 仅阻止下载，已导入凭据需停用用户或轮换身份才失效。共享入口的旧订阅仍可用，但不能绑定用户授权入站。

客户端模板明确选择 sing-box JSON、Mihomo JSON（YAML 兼容）、Shadowrocket Base64 URI 或 Xray JSON。可配置本地回环入口端口、手动选择或自动延迟选择及探测网址。协议出站由对应入站生成，不混入服务端私钥。Xray 模板使用第一个出站，暂不接受自动测速模板；URI 格式本地端口与策略由客户端自身设置。AnyTLS / Snell 不提供未经验证的 URI 格式。

## 验证与下一阶段

数据库集成测试覆盖双主机隔离、内核版本回报、REALITY 私钥隐藏、独立用户凭据、组权限撤销、用户停用、客户端模板和格式切换。23 组固定官方内核服务端/客户端实际转发测试覆盖新旧协议模板；额外测试一个内核中的两个入站、逐入站路由及移除用户后仍保持另一入站可用。

参考了 2s-ui 的主机/入站隔离与服务端/客户端分离，以及 Remnawave 的配置档、入站和用户组关系，未复制其源代码。参考链接：

- https://github.com/shenaba/2s-ui/blob/main/database/model/inbounds.go
- https://github.com/remnawave/backend/blob/main/prisma/schema.prisma
- https://xtls.github.io/en/config/transports/reality.html
- https://sing-box.sagernet.org/configuration/shared/tls/
- https://github.com/SagerNet/sing-box/blob/v1.14.3/common/tls/reality_client.go
- https://github.com/XTLS/REALITY/blob/83f621788ca8/tls.go

后续仍需：经核实的更多传输与协议字段、外部出站导入、逐用户流量采集与配额强制执行、设备数限制、管理员角色/会话/TOTP、多人租户和订阅页面、配置档跨主机复用与回滚 UI、FRP UDP 场景。当前不提供计费或虚假的流量限制。
