# 星枢 0.25.6

修复 Linux nftables 1.0.6 的 JSON 列表漏报表注释时，星枢将自己的端口跳跃表误判为外部表，导致规则更新、过期清理及节点删除一直等待的问题。

只在 JSON 缺少表注释时，读取同一张表的数值文本列表，并核验精确表名、inet 地址族及第一个表级注释。显式的外部标记、链或规则注释、名称前缀及重复身份均拒绝。保留 `nft --check`、原子规则替换和停止回执确认；不直接删库释放端口。

验证覆盖真实 Debian 12 nftables 1.0.6 与当前 Ubuntu：IPv4/IPv6 UDP 跳跃、重复应用、租约过期、主监听保留、同名外部表保护；同时运行完整后端、前端及安装升级验证。

## 已有主机修复

控制面升级不会直接替换其他 VPS 的辅助脚本。已启用端口跳跃的主机可在连接/恢复中生成“升级 Agent”命令，升级至 0.25.6；这会短暂重启该 Agent 管理的内核。已有主机也可使用下面的辅助脚本热修复，不重启 Agent 或代理内核。

在**发生故障的 Agent 主机**以 root 执行。命令锁定经过测试的提交和 SHA-256，备份旧文件后原子替换；不修改凭据、流量账本或其他规则表。旧安装使用 `/etc/harbor/agent.env` 时，已有 systemd 辅助服务仍使用原来的环境路径。

```bash
(
  set -eu
  target=/usr/local/libexec/starhub-agent-network.py
  test -f "$target"
  test ! -L "$target"
  f=$(mktemp)
  stage=$(mktemp /usr/local/libexec/.starhub-network.XXXXXX)
  trap 'rm -f "$f" "$stage"' EXIT
  curl --proto '=https' --tlsv1.2 -fsSL 'https://raw.githubusercontent.com/czy-code-923/czy-code/c9587e8bdf029289c99b634975ae7bdfa9fe49b2/internal/api/bootstrap/agent-network.py' -o "$f"
  printf '%s  %s\n' '39e064dbd52354f278ce37f3083dd7f893b987efc94a94b4f22e1da87922ea81' "$f" | sha256sum -c -
  python3 -c 'import ast,sys; ast.parse(open(sys.argv[1],encoding="utf-8").read())' "$f"
  backup="$target.before-nft106.$(date -u +%Y%m%dT%H%M%SZ)"
  cp -p "$target" "$backup"
  install -o root -g root -m 0755 "$f" "$stage"
  mv -f "$stage" "$target"
  printf 'Backup: %s\n' "$backup"
  systemctl reset-failed starhub-agent-network.service
  systemctl start starhub-agent-network.service
  systemctl status starhub-agent-network.service --no-pager -l
)
```

`Type=oneshot` 成功后通常为 `inactive (dead)`，上一次执行应显示 `status=0/SUCCESS`。刷新节点页确认原删除流程已完成；不需要伪造停止回执或重建用户。若仍失败，请查看该辅助服务日志，不能删除归属校验。
