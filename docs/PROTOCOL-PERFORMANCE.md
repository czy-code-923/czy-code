# 协议参数与性能设置

面板只暴露经校验的内核适配字段。配置能解析、协议能连接，并不代表线路一定可以跑满标称带宽；实际速度取决于客户端到 VPS 的路径、丢包、RTT、CPU、宿主机调度和商家限速。升级不自动修改已有节点、身份或系统网络参数。

## Hysteria2

所有带宽数值均为 **Mbps**，不是 MB/s；100 Mbps 的理论字节速率约 12.5 MB/s。

| 位置 | 字段 | 方向与含义 |
| --- | --- | --- |
| 服务端 | `up_mbps` | VPS 发出至客户端，Brutal 协商上限；0 不设置 |
| 服务端 | `down_mbps` | VPS 接收客户端上传，Brutal 协商上限；0 不设置 |
| 客户端 | `hy2_client_up_mbps` | 客户端上传到 VPS 的实测可用带宽 |
| 客户端 | `hy2_client_down_mbps` | 客户端从 VPS 下载的实测可用带宽 |

**两端不用填写相同数值**。服务器的 1 Gbps 端口不代表手机到服务器也能达到 1 Gbps。服务端上限只参与 Brutal 协商，不是所有用户共享的总限速，也不能用来限制 BBR 流量。

客户端模式 `hy2_client_mode`：

- `bbr`：新建表单默认值。订阅不输出客户端 `up_mbps/down_mbps` 或 Mihomo `up/down`，使用内核自动估计。适合移动网络和不确定可用带宽的情况。
- `brutal`：按实际可用带宽填写客户端上传 / 下载，至少一个方向必须大于 0；为 0 的方向仍使用自动估计。填得太低会限制速度，过高可能增加丢包、延迟和流量消耗。建议从低于稳定实测值的目标开始逐步验证，不根据 VPS 网卡标称值盲目填写。
- 空字符串：旧版兼容模式，仍将原 `up_mbps/down_mbps` 同值写入客户端。既有节点升级不会自动切换模式。API 调用方应显式选择 `bbr` 或 `brutal` 创建新配置。

`hy2_bbr_profile` 是客户端本地 BBR 行为，空值保留内核 `standard`。可选 `standard/conservative/aggressive`，不是带宽保证。服务端维持现有内核默认配置。默认不强制忽略客户端带宽。

自用通常可保持服务端两个值为 0，在客户端选择自动估计或填写实测带宽。用户配额和计费倍率另由权限与计量功能管理。

TLS 证书及 SNI 必须匹配。Salamander 密码、端口跳跃是可选项，启用时两端配置需一致；面板会生成完整订阅。端口跳跃还需要 Linux 受限助手、Agent 0.23.14+，以及 UDP 端口放行。普通 TCP 的系统 BBR 设置不控制 QUIC 的 BBR。

## 其他协议

| 协议 | 已暴露的专属设置 | 默认和限制 |
| --- | --- | --- |
| TUIC v5 | 拥塞控制、客户端 UDP native/quic 转发、客户端心跳 | 拥塞控制同时用于两端；心跳 UI 单位秒，Mihomo 输出转换为毫秒；保留原有 0-RTT 关闭。没有 HY2 带宽或端口跳跃字段 |
| AnyTLS | 客户端空闲检查间隔、空闲超时、最少保留会话 | 0 保留内核默认；保留数上限 32，增加会话会增加资源占用；服务端 padding 使用原默认 |
| Hysteria v1 | 上下行带宽、混淆密码、端口跳跃 | 两个带宽值必填，旧适配共用于两端；批量创建不再自动填 100 Mbps |
| Shadowsocks | 经典 AEAD / 2022 加密方式、TCP+UDP / 单网络监听 | 密钥长度随加密方式校验，原生监听与额外传输封装分开 |
| Snell | v4 兼容 / v6、HTTP 混淆及 Host、v6 整形模式 | 客户端和版本能力过滤保持；独立身份规则保持 |
| VLESS / VMess / Trojan | 内核适配的传输 / TLS / REALITY、Vision、路径、Host、gRPC 服务名、ALPN | 按协议、内核和客户端兼容矩阵校验；不套用 QUIC 的参数 |
| HTTP / SOCKS / Mixed / Naive | 认证、相应的 TLS 与专用客户端配置 | 不套用其他协议的拥塞或带宽字段 |

单个和批量创建使用相同的专属参数编辑器。未知组合与越界参数由后端再次校验；不会透传任意内核字段。

## 订阅、API 与 Agent

新增字段在 `LiveInput`、加密持久化、配置详情与模板能力 API 中一致，OpenAPI 源为 `internal/api/openapi.json`，文档副本 `docs/openapi.json` 同步。

Mihomo / sing-box 完整配置（含 Hiddify 等对应格式）以及 Mihomo 节点配置会同步已适配的客户端参数。纯 URI / 二维码节点格式没有通用的全部调优字段；客户端可能忽略扩展，因此面板分享检查会提示使用完整配置，不能宣称所有 URI 客户端已收到调优值。

客户端专属选项不会当作服务端入站参数下发。旧 Agent 可以继续运行既有协议；HY2 链式出站会将独立带宽转换成旧 Agent 已理解的字段。链式入口若还需应用新的 BBR profile、TUIC 心跳 / 转发方式或 AnyTLS 会话选项，需要 Agent 0.23.16+；旧入口会明确拒绝新增配置，避免静默忽略。

修改后客户端需刷新原订阅，或等待客户端自身的更新周期。面板不能强制刷新离线客户端。无需重新创建用户或订阅身份。

## 官方依据

- [sing-box HY2 入站](https://sing-box.sagernet.org/configuration/inbound/hysteria2/)
- [sing-box HY2 出站](https://sing-box.sagernet.org/configuration/outbound/hysteria2/)
- [Hysteria 带宽协商与拥塞控制](https://v2.hysteria.network/docs/advanced/Full-Server-Config/)
- [Hysteria 性能排查](https://v2.hysteria.network/docs/advanced/Performance/)
- [Mihomo TUIC](https://wiki.metacubex.one/config/proxies/tuic/)
- [sing-box AnyTLS](https://sing-box.sagernet.org/configuration/outbound/anytls/)
