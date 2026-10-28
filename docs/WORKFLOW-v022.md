# 0.22 订阅与流量修正

## 客户端与内核分开适配

| 选择 | 输出 | 策略与限制 |
|---|---|---|
| Clash / Mihomo / Clash Verge / FlClash | Mihomo YAML | 原生策略组、规则、DNS；Snell v4 模式，v6 过滤 |
| Hiddify | sing-box App JSON | TUN、selector、路由、DNS；保守过滤 Snell / Naive，不把服务端新版能力等同于 App |
| sing-box / SFA / SFI | 原生 App JSON | TUN、回环 Clash API；Snell 需客户端内核 1.14+ |
| Xray | 内核完整 JSON | 供自定义运行配置使用，不等于 App 的节点订阅 |
| v2rayNG / v2rayN / NekoBox / Happ / v2RayTun | Base64 URI 节点列表 | 可逐个选择兼容节点，路由/DNS 由 App 管理，无法携带 Mihomo 交互组 |
| Shadowrocket 节点订阅 | Base64 URI 列表 | 仅验证过的协议链接，Snell / AnyTLS 过滤 |
| Shadowrocket 分流配置 | conf | 单独规则配置，节点订阅配套导入；规则目标转为 PROXY |
| NaiveProxy | 专用 JSON | 必须且只能有一个兼容节点 |

官方资料：[v2rayN 订阅定义](https://github.com/2dust/v2rayN/wiki/Description-of-subscription)、[v2rayNG 导入源码](https://github.com/2dust/v2rayNG/blob/master/V2rayNG/app/src/main/java/com/v2ray/ang/handler/AngConfigManager.kt)、[Hiddify](https://hiddify.com/app/How-to-install-Hiddify-app/)、[Snell 1.14+](https://sing-box.sagernet.org/configuration/outbound/snell/)。不同平台和旧版 App 的支持范围仍以其内核为准。

先筛选已授权、运行中、已应用且兼容的端点，再编译策略。移除空策略组及其递归引用；失去目标的规则改为 REJECT，防止误走直连。DNS 的代理目标消失则明确阻止分享，不静默改变解析路径。未支持的格式无法无损表示安全/传输字段时过滤，不猜测转换。

没有兼容节点返回明确 422；没有运行节点返回 503。后台检查可显示实际客户端、包含/排除端点数量。IPv4 原名、IPv6 原名-v6，端点选择继续用稳定 ID。二维码只编码当前所选格式的订阅 URL，使用本地库生成，无远端转换服务；并非所有客户端支持扫描 URL，可复制链接导入。

## 流量计量

用户权限页可见时每 30 秒刷新，重新显示页面立即刷新，销毁组件停止轮询。Agent 原生用户计数约 10 秒上报。用户/API 增加 `metered_nodes` 和 `shared_entries`；后者列出已授权 SS/Snell 入口总量，明确属于全部使用者，不能计入某个人账单。

个人计费仍由内核认证身份产生，不从订阅来源 IP、客户端自报流量或网卡总量推算。共享密钥入口不能独立限额、撤销和识别个人。需要个人配额时使用获授权的独立 VLESS/VMess/Trojan/Hy2/TUIC/AnyTLS 等身份；已有共享流量不补记给个人。Clash 用量卡片需更新订阅后读取响应头，不是后台实时推送。

## 域名故障恢复

`scripts/panel-access.py status` 只读查询，支持现有 `/etc/harbor`、Linux quickstart 和 Windows 安装。默认不开放新端口。Linux `enable-ip --allow-http` 在已有控制面之外启动轻量网页反代，仅代理现有认证 API、静态前端，默认 30 分钟超时且不随重启启动。`--persistent` 才常开；`disable-ip` 只停本项目的恢复服务。

同源 IP 网页操作可用，其他来源继续经过 API 的显式 CORS 授权。不会更改主域名、订阅域名、管理员令牌、数据库或远程 Agent，也不会将 8080/8091/数据库直接暴露。IP HTTP 不加密；优先使用 SSH 隧道，需要公网故障恢复才短时开启。主机/云防火墙的 18081 规则须显式放行。Windows 使用 `--foreground`，超时/Ctrl+C 退出；不声称支持后台 systemd。Compose 使用既有端口映射或 SSH 转发。

卸载器会同时关闭并移除自己拥有的备用服务。状态查询不宣称 DNS 和上游网络一定可达。
