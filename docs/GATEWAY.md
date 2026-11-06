# 域名、证书与网关安装修复

`IP:18080` 能访问只表示控制面在运行。站点域名、免费证书、反向代理还需要 Nginx、Certbot、Cloudflare DNS 插件及星枢的受控网关助手。0.25.5 起 Linux systemd 一键安装会补齐这些组件；已有宝塔 / Nginx 优先兼容识别，保留其他站点。

## 旧版 Linux 面板补装（无需重装）

以 root 执行。普通用户先切换 root；root 不需要 sudo。命令读取已有的私有配置，不生成或输出管理员令牌、不改数据库和代理身份，不需要把任何令牌发到聊天中。

```bash
(
  set -eu
  f=$(mktemp)
  trap 'rm -f "$f"' EXIT
  curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/v0.25.5-distribution/scripts/install-gateway.py -o "$f"
  python3 "$f" --env /opt/starhub-control/control.env --install-deps --enable-acme
)
```

脚本下载固定正式版源码包，核对 GitHub 发布附件的 SHA-256 和大小，只提取指定的网关助手。现有 0.25.x 控制面可先补装，不必先升级控制面；更新到 0.25.5 后证书页还能显示助手心跳和依赖状态。

- Debian / Ubuntu 自动使用 apt 安装缺失依赖。dnf / yum 系统需要已配置可提供 Certbot DNS 插件的发行版仓库；依赖不可用会停止并保留控制面。其他系统先用发行版包管理器安装 Nginx、Certbot 及 Cloudflare DNS 插件，再去掉 `--install-deps` 重试。
- 默认新装配置为 `/opt/starhub-control/control.env`。旧 `/opt/harbor` 布局改成 `--env /etc/harbor/control.env`。
- 检测到仍在运行的旧 `harbor-domain-sync.timer` 或 `harbor-managed-gateway.timer` 时会拒绝增加第二套助手，应继续维护原助手。
- 已有宝塔 Nginx 使用 `/www/server/nginx/sbin/nginx` 与其 vhost 目录；已有系统 Nginx 使用当前程序和 `/etc/nginx/conf.d`。不覆盖已有非星枢配置。不认识的目录或其他程序占用 80/443 时停止，先按实际服务配置网关。
- 网关网页上游从私有配置中的实际 WEB_ADDR 端口读取，新装通常为 18080；兼容旧版单独网页服务的 5173。
- 继承现有原生网页监听范围：已公开的控制面可通过 HTTPS 域名公开登录；仅回环监听的安装保留管理来源限制。内部 API 和数据库仍使用回环地址。
- `--enable-acme` 明确允许执行面板中已经配置的域名签发与续期。Cloudflare 令牌只保存在后台加密配置和签发期间的私有临时文件中。

## 检查是否已运行

```bash
systemctl status starhub-gateway.timer --no-pager
journalctl -u starhub-gateway.service -n 60 --no-pager
```

助手约每分钟运行一轮。证书原先处于“等待签发”时会自动处理，不必删除重建。失败时检查日志与面板详情，修正域名或 DNS 凭据后再重试；证书重试有最短间隔限制。

## DNS 与 Cloudflare

- HTTP-01 要求正确的 A / AAAA 记录，验证入口公网 TCP 80 可达。面板 18080 端口不能代替 ACME 验证端口。
- Cloudflare DNS-01 不要求开启代理“小黄云”，也不要求 DNS 指向总管理机，但需要该域名使用 Cloudflare DNS、对应区域的 DNS 编辑权限令牌及 Certbot DNS 插件。
- 证书签发成功与网站可访问是两步。若站点使用 Cloudflare 代理，源站 80/443、Nginx 和匹配域名的证书仍需正确配置。源站 HTTPS 就绪后使用 Full (strict)，不要用 Flexible 回源到强制 HTTPS 的站点。
- 主站与独立订阅域名可分别配置，助手会分别处理。已有受管 Cloudflare 证书的主域名不会并发回退为 HTTP-01 申请。

## 安全与卸载边界

修复仅安装受控助手，不接受远端任意 Shell 或原始 Nginx 指令。签发串行执行，配置先做 Nginx 原生检查；失败恢复已有文件。后台更新期间会暂停平台自己的网关助手，避免更新文件与签发并发。

面板卸载会停止并移除星枢网关 service/timer，保留 Nginx 软件、其他网站与 Let's Encrypt 证书。卸载前在 Nginx / 宝塔中单独确认不再使用的面板域名入口。
