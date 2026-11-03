# Hysteria / HY2 端口跳跃

默认关闭，不改变现有节点。主机内核仍监听一个 UDP 端口；客户端周期切换到配置的入口范围，受限助手将 UDP 数据转发到原端口。IPv4 / IPv6 沿用节点监听配置。TUIC、VLESS、Trojan、SS、Snell 不复用 Hysteria 的跳跃参数。

## 使用

1. 更新控制面至 0.23.14，刷新网页。
2. 在目标 Linux 主机安装系统 nftables 包（Debian/Ubuntu：`sudo apt-get install nftables`；Fedora/RHEL 系：`sudo dnf install nftables`）。无需启动或接管系统 nftables.service，不刷新现有 ruleset。
3. 主机 → 连接 / 恢复 → 安装 / 更新端口跳跃助手；在该主机 SSH 执行生成的摘要固定命令。标准 `/etc/harbor-agent/agent.env` 和管理机旧布局 `/etc/harbor/agent.env` 均保留原身份、内核、环境和账本。命令会从同版本正式发行包校验并更新 Agent、受限助手及卸载助手。自定义 / Compose / Windows Agent 不自动安装 Linux 助手。
4. 新建、编辑或批量添加 Hysteria / HY2，在「UDP 端口跳跃」启用，填写如 `30000-30100,30200-30250`，间隔默认 30 秒（5–3600 秒）。每节点最多 16 段、不重叠的 4096 个端口；限制规则和数据库规模。
5. 监听设为 `0.0.0.0` 或 `::`。云安全组放行跳跃 UDP 范围，系统防火墙允许原 UDP 监听端口；可按现有防火墙策略同时登记入口范围。助手只维护独立 NAT 表，不修改安全组、INPUT 放行策略或其他 nft 表。
6. 保存并启用，等待 Agent 和本机助手回报后更新客户端订阅。助手未安装 / 缺少 nftables / 配置未回报时，跳跃节点不会被误报为可分享的运行节点。

## 生命周期与边界

- API 在创建、编辑、批量部署时原子预留全部监听与跳跃端口；拒绝与其他节点或库存实例冲突。修改范围时释放旧预留，删除仍等待 Agent 停止回执。
- Agent 先执行原生内核检查和本机端口占用预检。低权限 Agent 不获得 NET_ADMIN，不执行远程 shell；只写类型固定的私有请求文件。
- 独立 root 助手每 10 秒在请求文件存在时运行，只接受已验证的十进制 UDP 范围、固定本地监听目标和已安装主机身份。所有路径逐层禁止符号链接，状态采用排他临时文件；nft 命令由本机固定，先检查再原子应用。
- NAT 规则限制本机目标地址和非回环入口。仅维护带专有标记的 `starhub_hop_<host_id>` 表；同名第三方表拒绝覆盖，不执行 `flush ruleset`。
- 停用、删除、暂停和退出写入空配置；请求超 45 秒自动清除专有转发。请求文件清理后定时器保持闲置；设备卸载先清除其专有转发及助手。已有 conntrack 会话仍受实际代理进程和用户权限约束。
- 跳跃只是连接入口变化，身份与内核计量保持原实现；不创建额外内核进程，不把每个跳跃端口的流量重复累加。
- 自动更新控制面不自动替换远端 Agent；旧 Agent 保持已有功能，端口跳跃需要 0.23.14+。Agent 降级时跳跃配置不会下发为可运行状态。

## 客户端

Mihomo 输出 `ports: 30000-30100` 和数值 `hop-interval`。sing-box 输出 `server_ports: ["30000:30100"]` 和 `hop_interval: "30s"`，同时移除互斥的 `server_port`。Hysteria v1 跳跃要求 sing-box 1.12+；HY2 要求 1.11+。Hiddify 使用其兼容的 sing-box 配置，NekoBox 使用已适配的节点格式。纯 URI 客户端在原监听端口保持可用的前提下附带 `mport`；不识别的客户端会使用原单端口，不承诺其执行跳跃。原客户端协议兼容筛选保持不变。

官方依据：[sing-box HY2](https://sing-box.sagernet.org/configuration/outbound/hysteria2/)、[sing-box Hysteria](https://sing-box.sagernet.org/configuration/outbound/hysteria/)、[Mihomo HY2](https://wiki.metacubex.one/config/proxies/hysteria2/)、[Hysteria 端口跳跃](https://v2.hysteria.network/docs/advanced/Port-Hopping/)。

## 检查

`python3 scripts/test_port_hopping.py` 检查数值语言、跨节点冲突、归属标记和检查顺序。

仅在隔离 Linux 网络命名空间执行原生测试：`sudo unshare --net python3 scripts/test_port_hopping_native.py`。测试创建临时 veth / 子命名空间，实测 IPv4 / IPv6 UDP 范围与离散端口、租约到期删除转发、保留其他表及原监听；不会使用生产主机的网络命名空间。
