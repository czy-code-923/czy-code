# VPS 部署记录（2026-10-22）

目标：45.129.8.122，Debian 12 / amd64，1 核 / 约 2 GB 内存；域名 https://vps.iczy.top。

通过用户已有 ServerBox SSH/SFTP 会话部署。预编译 Linux 安装包经过 SHA-256 校验；保留现有 sing-box、宝塔站点和其他 Docker 容器。

## 已运行

- `/opt/harbor/bin/harbor-control`：普通系统用户 `harbor`，systemd 开机启动；API 127.0.0.1:8080、Web 127.0.0.1:5173、订阅 127.0.0.1:8091。内存硬上限 160 MB，Go 软上限 96 MiB。
- 独立 PostgreSQL 17 Alpine 容器 `harbor-postgres`，持久卷 `harbor-postgres-data`，仅 127.0.0.1:15432 发布；内存上限 192 MB，shared_buffers 32 MB、max_connections 20，应用连接池最多 4 条。
- `harbor-agent.service`：普通用户运行，主动连接本机控制面；官方 sing-box 1.14.3 / FRP 0.71.1 下载和 SHA-256 校验通过，开启受限 live runner。任务模拟模式与真实服务状态分开显示。
- 专用 Nginx 网关 `/www/server/panel/vhost/nginx/harbor-vps.conf`；独立有效 HTTPS 证书位于 `/etc/letsencrypt/live/vps.iczy.top/`。管理路径仅允许当前管理员来源 CIDR 及回环；订阅和 Agent TLS 路径独立路由。
- `harbor-domain-sync.timer` 每分钟检查后台域名/HTTPS 设置，证书检查节流 12 小时；失败回滚专用 vhost，并退避 30 分钟。关闭 HTTPS 默认拒绝应用，需 root 明确选择 `--allow-http`。
- `harbor-backup.timer` 每日备份到 `/var/backups/harbor`，包含数据库及私有设置；仓库提供 `scripts/backup-native.py`。当前仅为本机备份，尚无异地副本或自动保留策略。首个压缩备份的流式写入问题已修复，可读性已核验；旧的无效备份保留为 `.invalid`，不能用于恢复。

## 登录与维护

在自己的 SSH 终端取得管理员令牌，然后粘贴到控制台。不要将输出发到聊天、GitHub 或日志：

```sh
sudo sed -n 's/^ADMIN_TOKEN=//p' /etc/harbor/control.env
```

私有文件 `/etc/harbor/control.env` 与 `agent.env` 权限 0600；保留 MASTER_KEY，丢失会使已加密配置无法恢复。管理 IP 变化需要 root 更新域名同步服务内 CIDR。

```sh
systemctl status harbor-control harbor-agent --no-pager
systemctl list-timers 'harbor-*'
journalctl -u harbor-control -u harbor-agent --since '-15 min'
systemctl start harbor-backup.service
/www/server/nginx/sbin/nginx -t
```

停止 Harbor：`systemctl stop harbor-control harbor-agent harbor-domain-sync.timer`。先备份，再仅移开 Harbor 自有 vhost，通过 Nginx 检查后 reload；保留数据库卷和私有环境文件，不改动现有站点。

## 验收边界

- Edge 已打开真实 HTTPS 登录页，标准证书验证成功；外部 GET `/` 为 200，未认证管理 API 和 Agent 心跳为 401，无效订阅为 404。
- 数据库迁移、真实 Agent 心跳通过；首次数据库容器占用约 36 MiB，实际用量随实例数增加。
- 已通过真实 Linux Agent + 官方 sing-box 的 IPv4→IPv6 两跳加密转发、HTTPS 订阅三种格式、链接轮换与撤销，以及服务停止验收。测试服务仅绑定回环，结束后全部停用；后台保留验收记录。
- 官方 FRP 的 TLS 服务端/客户端实际转发与停止验收通过，使用同台 VPS 回环环境；没有假称用户真实内网设备已经接入。
- 实机发现并修复 Linux Agent 沙箱缺少 AF_NETLINK 的问题。仅补充普通用户网络接口监视需要的套接字族，CapabilityBoundingSet 为空，不授予网络管理能力。
- 防火墙策略保存与 Linux nftables 内核语法预检通过。页面草稿不代表内核规则已应用；保留现有宝塔和业务规则。
- 其他两台 VPS、用户无公网 IP 的设备，以及跨主机公网 IPv6/FRP 尚未连接或验收。本机回环测试不能代替这些部署。
- 此版本为基础版，不声称已经实现商业多租户、细粒度 RBAC/MFA、计费、流量配额、Xray 或完整运行版本回滚。
