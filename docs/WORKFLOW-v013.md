# 0.13 协议分层、通知与校验

## 主流程

主机 → 内核 → 协议 → 传输方式 → 传输安全 → 用户权限 → 客户端策略 → 订阅。单个编辑与批量编辑都使用 API `/protocol-templates` 返回的同一兼容矩阵；最终由 Go 编译器再次校验，不依赖前端隐藏选项。

## 当前实际适配

- sing-box 1.14.4-harbor.1：Shadowsocks、VLESS、VMess、Trojan、Hysteria v1/v2、TUIC v5、AnyTLS、Snell v4-compatible/v6、HTTP、SOCKS、Mixed、Naive。另有 FRP 服务端/客户端。
- Xray 26.10.21：Shadowsocks、VLESS、VMess、Trojan、HTTP、SOCKS。
- TCP/WebSocket/gRPC/HTTPUpgrade；sing-box HTTP/HTTP2；Xray XHTTP 与 VMess mKCP。QUIC 协议固定使用 UDP/TLS。
- TLS 使用匹配域名且受信任的 fullchain；VMess 可无外层 TLS。REALITY 在 sing-box 开放 VLESS TCP，在 Xray 开放矩阵中验证的 VLESS/VMess/Trojan TCP/gRPC/XHTTP；Vision 仍限定 VLESS TCP + TLS/REALITY。不得用大厂 SNI 代替普通 TLS 证书。
- SS 开放 AEAD 三种与 SS2022 AES-128/AES-256/ChaCha20 三种认证加密；AES-128 的 SS2022 密钥为 16 字节，其余 SS2022 为 32 字节。改变密钥长度时需显式轮换凭据。
- 新适配需要 Agent 0.13。旧 Agent 不会收到无法解析的新协议或链式出站。

这不是“任何协议可任意组合”。兼容目录覆盖两内核官方入站/出站目录，分别标记已适配、包装层、网络设备/隧道能力与未适配。ShadowTLS 需要内层协议，TUN/TProxy/Redirect/WireGuard 等需要独立权限和网络设备模型；当前不能当普通节点模板部署。Mieru/SSR/Juicity 等不因名字出现在其他项目就被伪装成这两个内核原生支持。

客户端订阅按实际能力筛选；XHTTP/mKCP 和当前 Xray REALITY 使用完整 Xray JSON，不冒充通用 URI。捆绑 sing-box 未编译 Naive 出站；Naive 使用专用 NaiveProxy JSON，要求仅一个兼容节点、连接地址为证书域名或通过 host-resolver-rules 映射的单个 IP，且明确忽略策略组。Naive 不能作为捆绑 sing-box 的链式下一跳。客户端具体 App 还受其内置内核版本限制。

## 配置与策略

服务器 JSON/YAML 可以识别为 sing-box、Xray 或星枢参数草稿，并选择多个入站中的一个应用。不会读取证书路径，不执行配置里的程序。多账号、路由、插件、文件路径及其他未迁移字段会提示核对，不能宣称任意原生配置可以无损往返。凭据只写，服务器 REALITY 身份由控制面生成。

策略表单在保存时自动同步 YAML；两处均修改时必须选择同步方向。嵌套组使用缓存去重，拒绝环路。手动客户端路由引用不存在的节点/组会报错。无可用节点按拒绝处理，不能回退直连。不能等价转换的规则返回错误；URI 与 NaiveProxy 单节点配置不承载分流策略。

新增端口占用在停止当前内核前预检。只有现有活跃成员的配置/账号完全相同时，新增失败才保留旧进程；撤权、停用、改凭据等保持 fail-closed。实际绑定仍可能与其他进程竞争，不承诺无中断原子热更新。

HTTP/SOCKS/Mixed/Naive 在用户列表为空时仍保持认证，防止空列表在原生内核中意味着免认证。HTTP/SOCKS 在两内核上有真实用户流量测试。Xray HTTP/SOCKS 部分日志不包含用户 ID；只按来源和入口展示元数据，用户流量仍来自原生计数，不猜测日志所属用户。

## Telegram

设置 → 通知与 Telegram。填写 BotFather Token 和数字 chat_id，先与机器人开始聊天。默认停用。Token 加密存储、不回显、不写日志。预览不发送；测试按钮明确发送到绑定聊天。

只读 `/status /hosts /traffic /users /help` 按命令、chat_id 和 Telegram 用户 ID 三重限制，拒绝机器人消息、超过 2 分钟的旧消息和多词命令。每轮最多回复 3 条，offset 持久化防重放。无 Shell、启停、删除、密码、API 密钥或订阅链接命令。

定时报告可选择状态、主机、用户用量、用户额度，15 分钟至 7 天，0 关闭。停机后不补发积压轮次。主机离线/恢复、用户额度阈值、证书不足 14 天事件每分钟检查，状态去重，每轮最多 5 条。网络重试存在重复投递可能。日志最多 7 天/500 条，UI 最近 100 条。长轮询不新增公网端口，多控制进程以数据库锁避免同时轮询。

一个 Bot 不能同时供其他进程长轮询或使用 Webhook。本平台不自动删除其他 Webhook；API 冲突会回报。未绑定真实机器人时，仅能验证模拟 Bot API 与权限、调度逻辑，不能声称已发送真实 Telegram 通知。

## 防火墙

编辑保存为新版本，旧版本归档；取消使用/归档、回收站删除、恢复均有 API 与审计。后台草稿操作不等同于远端内核规则改变。预览可以复制对应主机的受控移除命令：

```sh
sudo /opt/harbor/bin/harbor-firewall remove --host-id HOST_ID
sudo /opt/harbor/bin/harbor-firewall confirm --id TRANSACTION_ID --token CONFIRM_TOKEN
# 取消仍待确认的 apply/remove；超时也会自动恢复
sudo /opt/harbor/bin/harbor-firewall cancel --id TRANSACTION_ID
```

remove 只删除带本平台所有权标记的表，先保存快照、武装独立回滚定时器。默认 60 秒，未确认恢复。不会 flush ruleset，不管理云厂商安全组、Docker NAT 或其他防火墙表。Agent 保持无权限修改系统防火墙，面板不会接受任意 Shell。

## API 与安全检查

新增 `notifications:read/write` 权限；配置识别用 `nodes:write`，防火墙操作用 `firewall:write`。完整路径与字段见 OpenAPI，控制面/Agent 使用共享版本常量。外部集成使用最小权限密钥，不交付根令牌。

验证包括 PostgreSQL、原生内核服务端与客户端矩阵、真实转发及用户流量、无授权用户认证、增量部署端口冲突、Telegram 接口模拟与越权/旧消息/重复消费、防火墙版本/回收站/回滚、前端交互，以及 GitHub 全新 Linux 机器的安装-接入-部署-卸载。依赖扫描无结果不等于所有代码、第三方内核或未来版本不存在漏洞。
