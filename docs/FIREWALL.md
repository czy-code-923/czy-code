# 防火墙：策略管理与受限 Linux 应用

v0.3 附带了防火墙策略管理和独立的 `harbor-firewall` 工具。Web API 只保存策略版本、归档及预览，**不会执行内核修改，也不把保存成功显示成规则已生效**。真实应用由目标 Linux 主机上的本地 root 使用受限工具显式完成，不给通用 Agent 提权。

## 能力与保护

- 每主机不可变策略版本；IPv4 / IPv6 CIDR、TCP / UDP、单端口 / 范围、允许 / 拒绝；拒绝优先；可归档恢复。
- 固定保留回环、已建立/关联连接、必要 ICMP/ICMPv6 和 DHCP 客户端应答。
- SSH 只允许明确的管理员 CIDR，不允许 `/0`；显式保护 SSH、22、8080、5173，用户规则不能覆盖。管理 Web 端口仅允许回环。
- 每台主机仅管理 `inet harbor_<完整主机ID>` 表；已有同名表必须带 `harbor-managed-v1` 所有权标记。没有 `flush ruleset`，不清空其他表。
- 受限工具只允许 nft 固定参数和 systemd-run/systemctl 固定操作；不接收 native 脚本、shell 命令、插件、URL 或通用执行指令。
- 使用 `nft --check` 预检；先备份自有表并启动独立 systemd 回滚定时器，确认定时器 active 后才写入；60 秒内未确认自动恢复原自有表，第一次部署则删除自有表。
- Linux 使用内核文件锁，父进程被终止也会释放锁，不会因为留下锁文件阻止定时回滚。未结束的事务禁止重叠应用。

**覆盖范围是主机 INPUT 入站层**。不控制云供应商安全组、Docker FORWARD/DNAT、NAT、容器网络、出站策略或流量清洗/DDoS。其他防火墙仍可能拒绝本表允许的流量；本表的 accept 不能覆盖其他表的 drop。既有连接不会被新的拒绝规则主动断开。

## 页面使用

防火墙 → 新建策略 → 选择主机 → 填写真实 SSH 端口及管理员来源网段 → 添加业务端口 → 保存 → 预览 / 下载。

页面中的 `192.0.2.0/24`、`2001:db8::/64` 示例是文档保留网段，不能当作实际管理员来源。原生实验代理当前绑定回环，创建允许规则也不会让它自动成为公网代理。

“新版本”会复制旧策略供编辑，保存后新增版本；归档不修改主机上的规则。控制面暂不回传此独立工具的真实执行状态，不能从页面版本列表判断内核生效与否。

## Linux 本地运行

要求带 nftables 与 systemd 的 Linux、root、可信 root 所有且非组/全局可写的工具和状态路径。本次 Windows 开发环境没有可用 Linux 内核，**未真实执行 nft/systemd 应用或验证内核语法**；已提供隔离网络命名空间 CI 检查步骤，但该 CI 本次未运行。先在有控制台恢复通道的测试主机验证。

```sh
go build -o harbor-firewall ./cmd/firewall
sudo install -o root -g root -m 0755 harbor-firewall /usr/local/bin/harbor-firewall
# policy.json 是页面“下载受限工具策略”的 JSON，不是直接粘贴 nft 片段
harbor-firewall preview --file policy.json
sudo /usr/local/bin/harbor-firewall check --file policy.json
```

应用时填写当前管理连接的真实源 IP，工具会校验它被管理员 CIDR 覆盖；保留 SSH_CONNECTION 环境时，还会比对当前 SSH 服务端端口：

```sh
sudo --preserve-env=SSH_CONNECTION /usr/local/bin/harbor-firewall apply \
  --file policy.json --ssh-peer YOUR_CURRENT_SSH_SOURCE_IP --timeout 60
```

成功后输出事务 ID、一次性确认 token 和期限。**先另开一条全新的 SSH 连接确认可达**，再在期限内执行：

```sh
sudo /usr/local/bin/harbor-firewall confirm --id TRANSACTION_ID --token CONFIRM_TOKEN
# 需要立即撤销尚未确认的事务时：
sudo /usr/local/bin/harbor-firewall rollback --id TRANSACTION_ID
```

即使原 SSH / 父进程退出，systemd 定时器也独立运行；确认后定时器触发会读到 confirmed 并退出。不能确认的事务会回滚；如回滚失败，保留 pending 日志，必须通过控制台恢复。状态默认位于 `/var/lib/harbor-firewall`，0700、root 所有，文件0600。

原子替换仅限自有表；外部 root 同时修改规则、systemd 停止、内核崩溃、磁盘故障或管理员改写本地日志不在自动恢复保证内。此版本不安装开机规则持久化单元，重启后应重新核验；不要自行把未经核验的规则加入开机加载。

## 测试范围与依据

测试覆盖模板保护、注入、CIDR、端口范围、冲突、未保护 SSH 来源、回滚计时器先于应用、计时器失败不改规则、确认 token、回滚恢复及确认后定时器空操作。内核调用在测试中用隔离后端替身，不等同于真实 nft 通过。

依据：[nftables 官方手册](https://www.netfilter.org/projects/nftables/manpage.html)、[原子规则替换](https://wiki.nftables.org/wiki-nftables/index.php/Atomic_rule_replacement)、[systemd-run 独立定时服务](https://www.freedesktop.org/software/systemd/man/latest/systemd-run.html)。
