# 星枢 0.25.7 · 协议握手与封装配置完善

VPS 协议配置的新增、编辑、参数 JSON、原生配置识别、加密保存、Agent 下发和订阅导出统一支持新增字段。现有协议和默认参数保持兼容，无需迁移数据库，不主动轮换 UUID、密码、REALITY 密钥或改变用户组授权。

## 本次补齐

- VLESS / VMess / Trojan / AnyTLS / HTTPS CONNECT 的客户端浏览器指纹：默认、Chrome、Firefox、Safari、Edge、iOS、Android、randomized。REALITY 继续使用原有专用指纹字段与已验证的内核限制。
- Xray XHTTP：auto、packet-up、stream-up、stream-one，填充字节数或范围，以及流式客户端的 noGRPCHeader。路径与 Host 保持原有设置。
- Xray gRPC：客户端 multiMode；保留服务名和安全层选择。
- Xray mKCP：Seed、数据包头、MTU、TTI、上 / 下行容量和拥塞控制。容量单位为 MB/s，不能与 HY2 的 Mbps 混用。
- 补齐 sing-box HTTPS CONNECT 客户端 ALPN 导出，明确 Naive 的 Chromium 握手及专用 JSON、QUIC 协议的 TLS 范围。
- 修复链式配置可能把 XHTTP 等不可表达的下一跳交给 sing-box 的问题；不支持的组合在保存时返回原因。

## 现有协议检查范围

| 协议 | 编辑器主要设置 / 封装范围 |
| --- | --- |
| Shadowsocks | 经典 AEAD / 2022 密码算法、原生 TCP / UDP、适用算法的独立用户身份 |
| VLESS | UUID、Vision、TCP / WS / gRPC / HTTPUpgrade / HTTP2 或 Xray XHTTP、TLS / REALITY 按内核矩阵选择 |
| VMess | UUID、AEAD、可选外层 TLS、上述承载及 Xray mKCP；新增浏览器指纹与封装参数 |
| Trojan | 密码、适配的 TCP / HTTP 承载、TLS 与 Xray 允许的 REALITY 组合 |
| Hysteria v1 | QUIC / TLS、认证、上下行带宽、独立混淆密码、端口跳跃 |
| Hysteria2 | QUIC / TLS、Salamander、服务端带宽与客户端 BBR / Brutal、端口跳跃 |
| TUIC v5 | QUIC / TLS、UUID + 密码、拥塞控制、UDP 中继模式、心跳；不添加不存在的端口跳跃选项 |
| AnyTLS | TLS、密码、空闲会话检查 / 超时 / 数量，以及新增浏览器指纹 |
| Snell | v4 兼容 / v6、HTTP 混淆和 Host、v6 整形模式、适用客户端范围 |
| HTTP / HTTPS CONNECT | 用户名 / 密码、可选 TLS、SNI / ALPN；浏览器指纹仅导出到支持的 sing-box / Xray JSON |
| SOCKS / Mixed | 认证、原生 TCP / UDP 能力与对应客户端格式 |
| Naive | 用户名 / 密码、域名证书、TCP + TLS + HTTP/2、NaiveProxy 专用 JSON；浏览器握手由 Chromium 客户端实现 |

传输封装不等同于代理协议。编辑器继续使用服务端同一份内核兼容矩阵，避免展示无法执行的协议 / 承载 / 安全层组合。Naive 当前不作为内核链式下一跳；XHTTP / mKCP 和启用 multiMode 的 gRPC 使用完整 Xray JSON。Mihomo HTTP 没有浏览器指纹适配，不会把该选项作为有效配置静默导出。

## 升级与旧配置

1. 现有节点不填写新对象时保持原有生成结果；新选项默认留空 / 0 / 关闭，沿用内核默认。
2. 新字段位于 `transport_options`。旧 API 编辑器省略该对象会保留已存参数；传入 `{}` 可明确清除。未知字段和无效组合会被拒绝。
3. 单纯为普通 TLS 节点设置客户端指纹，不要求升级该服务端 Agent。实际执行新 XHTTP / mKCP 参数的 Agent，以及引用任意新参数的链式入口，需先升级到 **0.25.7+**。不使用新选项的旧 Agent 可继续运行。
4. 即使 Agent 降级，控制端也不会把未知字段发送给其严格 JSON 解码器；不支持的新配置停用，其他既有服务仍可同步。
5. 不变更现有证书验证策略，不导出服务器 TLS / REALITY 私钥。

## 验证

- Go 静态检查、全量后端测试及隔离 PostgreSQL 集成回归。
- 前端类型检查、生产构建与 81 项编辑器 / 功能回归测试。
- 面向全部客户端目录的混合协议订阅分发、参数完整性、已有订阅 URL 与用户身份回归。
- 固定版本 sing-box 1.14.4、Xray 26.10.21、Mihomo 1.19.33 原生配置校验。
- 真实 sing-box 客户端 / 服务端转发覆盖五种 TCP/TLS 协议的 Chrome / Firefox 指纹；真实 Xray 转发覆盖四种 XHTTP 模式、gRPC multiMode 与自定义 mKCP；测试保留 TLS 验证，仅信任隔离测试证书。
- 完整分发包继续由发布工作流完成 Linux 检查、四平台编译、原生协议检查、安装与卸载验证后发布。

这些是隔离环境的源码与内核验证，不代表已部署或验证用户的线上 VPS 网络、CDN、目标站或第三方客户端。

参考：[sing-box TLS](https://sing-box.sagernet.org/configuration/shared/tls/)、[XHTTP 官方说明](https://github.com/XTLS/Xray-core/discussions/4113)、[gRPC](https://xtls.github.io/config/transports/grpc.html)、[mKCP](https://xtls.github.io/config/transports/mkcp.html)、[Mihomo HTTP 实现](https://github.com/MetaCubeX/mihomo/blob/v1.19.33/adapter/outbound/http.go)。
