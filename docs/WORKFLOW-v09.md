# Harbor 0.9：四步管理流程

1. **连接主机**：登记 VPS，复制一次性 Agent 命令执行。Agent 主动连接控制端，不需要公开管理端口。主机名称、地址和标签可编辑；删除会撤销 Agent 与安装票据，保留审计和历史配置。仍有协议或未结束任务时拒绝删除。
2. **节点配置**：一个节点配置直接对应一台 VPS 上的一个协议入站。内核、入站、出站、链式路由作为同一主机配置自动编译和下发。同主机同内核一个进程。逻辑节点 ID 是内部兼容关系，无需另外创建或分组。
3. **用户权限**：用户有独立 UUID / 密码，通过用户组选择允许的入站。在用户编辑弹窗可直接分配组。删除用户同步删除其订阅并撤销服务端身份；其他用户不受影响。
4. **订阅分享**：选择用户和可选客户端模板，分享域名链接；允许编辑名称、期限和模板，以及复制、重置、删除。模板仅配置客户端，不是另一个部署层。SS / Snell 的共享密钥配置使用单独的共享订阅入口。

域名 / HTTPS、证书、反代、防火墙、审计在系统设置中。主界面没有暂存实例、暂存配置或额外的逻辑节点步骤。旧数据和 API 保留用于兼容和审计。

## 协议与组合

| 协议 | 服务端内核 | 配置 | 订阅格式 |
|---|---|---|---|
| Shadowsocks | sing-box / Xray | 2022 AES-256-GCM；AEAD AES-128/256-GCM、ChaCha20-IETF-Poly1305 | sing-box、Mihomo、Shadowrocket、Xray |
| VLESS | sing-box / Xray | TLS TCP / WS / gRPC；REALITY TCP；可选 Vision | 取决于安全模式；下述限制适用 |
| VMess AEAD | sing-box / Xray | 可选外层 TLS；TCP / WS / gRPC | 四种格式 |
| Trojan | sing-box / Xray | TLS；TCP / WS / gRPC | 四种格式 |
| Hysteria2 | sing-box | TLS、UDP；可选 Salamander 与上下行 Mbps | sing-box、Mihomo、Shadowrocket |
| TUIC v5 | sing-box | TLS、UDP、UUID + 密码、拥塞控制 | sing-box、Mihomo、Shadowrocket |
| AnyTLS | sing-box | TLS + 密码；原生默认 padding / 会话参数 | sing-box、Mihomo JSON |
| Snell | sing-box | v5 服务端提供 v4 兼容 TCP/UDP；HTTP 混淆；v6 加密整形 | v4 兼容：sing-box / Mihomo；v6：sing-box |

- 所有常规协议凭据自动生成，也可手动指定或轮换。Snell 无普通 TLS 证书；AnyTLS / Trojan 需要证书；VMess AEAD 自身不要求证书。
- 新协议选项和 Xray SS 要求 Agent 0.9+；旧 Agent 不得忽略字段后启用错误配置。默认的旧配置继续兼容。
- 共享密钥的 SS / Snell 不宣称支持独立用户撤销；撤销分享链接不等于撤销已导入的凭据。其他六类协议支持按用户组独立授权。
- 当前固定 Xray 26.10.21 的 REALITY 仅通过 Xray JSON 客户端测试；sing-box REALITY 供 sing-box / Mihomo 等使用。禁止已知不兼容导出和链式组合。
- Snell 没有实现 v5 QUIC；v6 不向不支持的客户端伪装成 v4。Shadowrocket URI 未开放 Snell / AnyTLS。没有把内核文档中的其他协议标为已实现。
- TLS 证书必须匹配 SNI 且受公开 CA 信任；REALITY 的 SNI 必须匹配实际握手目标，不能代替普通 TLS 的证书。

## 修改与删除语义

- 协议修改使用预期版本检查；物理主机和协议种类固定，可修改端口、地址、内核、安全、链式出站等。要迁移主机或替换协议，创建新配置后调整授权。
- 删除运行中的协议返回 202，先下发禁用版本。保持端口预留，直到同一主机 Agent 回报该版本 `stopped`，才移除组授权、订阅引用、FRP 分配和端口。旧版本回执不能触发删除。离线主机显示等待停止，不伪报成功。
- 被下一跳或 FRP 客户端引用的配置返回 409；先修改引用方。FRP 编辑需要确认停止；有客户端的 FRP 服务端不能直接改变连接契约。
- 用户 / 用户组权限变化在下一次 Agent 同步生效；Agent 失去控制面连接超过 90 秒会停止服务。删除订阅只停止下载。
- 删除客户端模板后，关联订阅使用默认设置；不改变用户权限。
- 新订阅令牌加密持久化，只有单独管理员链接 API 解密；列表不包含令牌。旧版仅存散列的链接需重置一次；重置后旧 URL 失效。更换域名时复制 API 使用当前域名。

## 验证与资源

原生测试运行实际 sing-box / Xray 服务端与客户端，对回环 HTTP 目标转发；包含 IPv4、IPv6、FRP、用户撤销、完整主机内核编译、Snell HTTP / v6、HY2 Salamander 与常见传输组合。数据库测试覆盖停止回执、端口释放、用户删除和链接恢复。

前端按页面加载代码；主机页仅轮询主机列表，隐藏标签暂停请求，节点页不重复下载兼容目录。Agent 和内核保持有界配置、快照与任务队列；不开放任意 Shell。

参考官方配置：
- https://sing-box.sagernet.org/configuration/inbound/snell/
- https://sing-box.sagernet.org/configuration/inbound/anytls/
- https://sing-box.sagernet.org/configuration/inbound/hysteria2/
- https://xtls.github.io/en/config/inbounds/trojan.html
- https://wiki.metacubex.one/config/proxies/snell/
