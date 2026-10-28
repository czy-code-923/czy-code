# 协议、封装、监听网络与安全层

这些是不同维度：**内核 → 代理协议 → 承载 / 封装 → 安全层**，另有入站套接字监听 TCP / UDP、客户端代理的 TCP / UDP 业务。TCP 封装不代表不能中继 UDP 业务，QUIC 的底层 UDP 也不代表只能代理 UDP 网站流量。

|平台已适配协议|底层连接 / 套接字|业务和本版修正|
|---|---|---|
|Shadowsocks AEAD / 2022|原生 TCP、UDP，默认同时开启|新增 listen_network：tcp,udp / tcp / udp；sing-box / Xray 入站、预检和导出同步。UDP-only 不提供 TCP 网页代理|
|VLESS / VMess / Trojan|TCP、WS、gRPC、HTTPUpgrade；按内核支持 HTTP2 / XHTTP / VMess mKCP|代理 TCP / UDP 业务，业务 UDP 可以封装进 TCP 等连接。安全层按内核矩阵联动|
|Hysteria v1 / Hysteria2 / TUIC|QUIC / UDP，TLS|代理 TCP / UDP；业务端口需放行 UDP，不应在封装选择中添加假 TCP 模式|
|SOCKS5 / Mixed|TCP 控制连接 + UDP 关联|修复 Xray 入站强制关闭 UDP、Mihomo SOCKS5 导出 udp=false。UDP 关联在已认证的 TCP 会话中建立，关联端口由内核分配|
|HTTP / Naive|本适配为 HTTP CONNECT / TCP、Naive HTTP2/TLS|不声称实现 CONNECT-UDP / HTTP3；Naive 使用专用客户端|
|AnyTLS|TLS / TCP 会话|UDP 中继取决于客户端 UDP-over-TCP 支持，不是原生 UDP 入站|
|Snell|当前 sing-box v4 兼容 / v6 适配使用 TCP 会话|中继 TCP / UDP，未实现 Snell v5 QUIC；不同版本客户端导出继续按实际能力筛选|

SS 的网络设置与高级参数 JSON、原生配置识别相互同步。SS 新监听设置需要 Agent 0.19；旧 Agent 会明确拒绝新设置，下发时也不允许静默忽略。未设置字段的旧节点继续默认双向网络，不强制改变历史配置。URI 订阅未必能表达 UDP-only 的特殊用途，优先使用客户端原生配置；本版不把不兼容组合伪装为可用节点。

不同内核 / 客户端版本支持程度不同，平台的矩阵表示已经适配的组合，不是上游所有功能的集合。导出成功只说明配置符合该格式，仍须等待部署回执、确认发布地址和业务端口可达；防火墙和云安全组也必须放行相应网络。

核验来源（2026-10-28）：[sing-box SS 监听网络](https://sing-box.sagernet.org/configuration/inbound/shadowsocks/)、[Xray SS 网络语义](https://xtls.github.io/en/config/inbounds/shadowsocks.html)、[Xray SOCKS UDP](https://xtls.github.io/en/config/inbounds/socks.html)、[sing-box SOCKS 客户端](https://sing-box.sagernet.org/configuration/outbound/socks/)、[sing-box Snell 版本配置](https://sing-box.sagernet.org/configuration/inbound/snell/)。
