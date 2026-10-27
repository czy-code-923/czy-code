# 0.12 工作流与边界

主机 → 内核和协议入站 → 用户组权限 → 客户端策略 → 订阅。协议不是内核，服务端入站和客户端出站分别生成。

## 地址与出站

主机心跳回报网卡 IP。创建协议默认填主机地址；可同时发布一条 IPv4 和一条 IPv6，或手动填写域名。IPv6 发布需 `::` 监听，系统和防火墙仍须支持 IPv6。双地址只增加客户端节点条目，同主机同内核仍合并为一个进程。主机没有回报 IPv6 时显示“无 IPv6”，不会猜测 AAAA 或宣称已具备公网 IPv6。

“VPS 出站”控制到目标站或下一跳的连接地址类型。未选为系统默认，单选限制类型，双选按顺序优先解析。sing-box 使用本机 DNS resolver；Xray 使用 ForceIPv4 / ForceIPv6 / ForceIPv4v6 / ForceIPv6v4。Xray 双栈的回退发生在解析无结果时，首选 IP 连接失败不会自动切换另一类型。与下一跳字面 IP 冲突时拒绝保存。Agent 低于 0.12 时拒绝设置新出站策略。

## 授权与计量

用户组可同时选择 VLESS、VMess、Trojan、HY2、TUIC、AnyTLS，以及 SS / Snell。独立用户协议使用用户身份计量并执行个人额度；SS / Snell 使用入口共享密钥，入口流量显示在共享服务统计中。不能区分某个用户的共享流量，不能靠撤销订阅或停用用户使其已经缓存的共享密钥失效；撤销共享访问需轮换入口密钥或停用入口。不会将整个共享入口流量重复计入每位用户。

每月重置日为 1–31，北京时间；本期设定 5 日，下个月 5 日重置。31 日遇到短月取最后一天，之后仍回到 31 日。旧周期起点保留小时与分钟，新指定日保留设置时的周期时间；首次重置在设置后的下个月。重置清空本期上/下行与已用量，累计流量、访问元数据保留。Clash 卡片用量由 Subscription-Userinfo 响应头提供，需更新订阅后刷新。

## 策略和客户端

每个“用户 + 客户端”最多一个策略。优先级：该用户该客户端 → 该用户通用 → 全局该客户端 → 全局通用。共享订阅只使用全局策略。

Mihomo YAML 只包含 proxy-groups 与 rules；节点身份由授权生成。可视化表单修改后点击“表单 → YAML”，保存以 YAML 为准。可用引用：`ALL`、`node:ID`、`host:ID`、`protocol:vless`、`family:ipv4`、`family:ipv6`、其他策略组、DIRECT、REJECT。指定节点使用稳定 ID；普通节点名在保存时绑定 ID。节点重命名不改变绑定。未获授权、停用、离线或未应用的节点不会发布。无可用规则目标改为拒绝连接并产生诊断提示。

|客户端选项|导出|策略语义|
|---|---|---|
|Mihomo / Clash Verge / Stash|Mihomo YAML|select、url-test、fallback、load-balance 原生；Stash 的协议支持取决于应用版本|
|sing-box / SFA / SFI / Hiddify / NekoBox|sing-box JSON|select → selector，url-test → urltest；需兼容生成配置的内核版本|
|Xray / v2rayNG / v2rayN|完整 Xray JSON|select 固定第一成员，url-test → leastPing balancer；不能提供 Mihomo 式交互选择|
|Shadowrocket / 通用 Base64 / v2rayNG URI / NekoBox URI|URI 节点列表|不携带分流组与规则，需明确选择仅节点模式|

`?client=` 显式选择应用时过滤其基础格式不支持的协议；`X-Starhub-Excluded-Nodes` 返回过滤数量。旧 `?format=` 保留严格模式，不会悄悄丢协议。客户端名称选项代表配置格式，不保证任意历史应用版本均有相同协议能力；Snell 等新增能力需要支持该协议的内核。已用 sing-box 1.14.4、Xray 26.10.21、Mihomo 1.19.30 原生配置检查，未在每款第三方手机应用上逐一实机测试。

DOMAIN、DOMAIN-SUFFIX、DOMAIN-KEYWORD、IP-CIDR、来源 IP、端口、TCP/UDP 等可转换。PROCESS、GEOIP、GEOSITE、RULE-SET 等存在平台或规则数据依赖，无法等价转换时给出 422；为该客户端配置专用策略或 native_options，不静默丢弃。订阅关联的客户端模板覆盖最后生效；可编辑 DNS、路由、回环入站等，凭据和节点列表不可任意替换。

## 入口

后台域名可留空；`ip_origin` 为明确配置的 `http(s)://IP:端口`，不信任请求 Host 或 X-Forwarded-Host 生成分享链接。订阅另有 share_domain / share_https，留空继承主站入口。已设置独立分享域名时，受控网关只对该域名开放 /s/，后台 API 不在分享域名提供。

原生 WEB_ADDR 默认 127.0.0.1:5173，可明确配置公网网页端口；8080 API、8091 分享处理器和数据库保持回环。domain-sync 支持独立分享 vhost 与无域名的 IP HTTP 端口；root 同步器须明确 `--allow-http` 才应用 HTTP。管理端一键连接默认要求 HTTPS；无域名 HTTP 必须在向导勾选明文风险后生成命令。

## 主机卸载

Linux 一键接入会安装 root 拥有的固定卸载器与 15 秒本地标记检查 timer。Agent 本身仍以普通用户运行，无任意 shell、命令字段或远程删除路径。卸载器只读取本地 root 清单，允许 Agent 安装目录，拒绝删除控制面、数据库和其他软件。管理端本机也只删除 Agent 相关文件。

POST /hosts/ID/retirement 请求卸载，离线时保留 requested。Agent 停止自己的进程，固定 helper 核验请求、停止 Agent 服务、清理文件，再发送 ack；后台收到 ack 才清理部署、用户组节点关联、共享节点关联、任务、配置、流量和访问记录并撤销 Agent 身份。审计与不含 IP/配置的墓碑保留。确认回执丢失可重复提交；失败保留 root 私有凭据供重试，不把“失联”当作“已删除”。存在其他主机的链式 / FRP 依赖时拒绝卸载。

未连接的库存主机可本地删除。旧 Agent 或没有安装受控 helper 的手动 Windows / Linux Agent 不假报卸载成功，会要求升级。Windows 旧登录任务仍按 INSTALL.md 手动卸载，未冒充受控 Linux 卸载能力。

## 安装、更新

GitHub 附件提供 Linux / Windows、amd64 / arm64 四种完整二进制包、源码、Agent 包、固定摘要安装脚本和 SHA256SUMS。新建控制面一键脚本需要已运行的 Docker 与 Python 3，不会静默改系统 Docker、覆盖既有服务或自动删除用户数据。源码与 Compose、离线、前台 / OpenRC、Windows 登录任务方式见 INSTALL.md。新安装器不自动配置系统防火墙或签发域名证书；已有管理端的受控 HTTPS 网关继续提供域名与证书管理。
