# 面板地址查询与 IP 恢复命令

## 先选择安装布局和执行身份

终端显示 `root@主机名:~#` 时已是 root，直接使用 `python3`，无需为这些命令安装 sudo。普通用户可先执行 `su -`；系统已配置 sudo 时，也可在 Python 命令前加 `sudo`。

| 安装方式 | 脚本 | 私有配置 |
| --- | --- | --- |
| 一键新装 Linux，默认目录 | `/opt/starhub-control/panel-access.py` | `/opt/starhub-control/control.env` |
| 旧 Linux / 宝塔部署布局 | `/opt/harbor/panel-access.py` | `/etc/harbor/control.env` |
| 自定义 `--prefix` | `实际安装根目录/panel-access.py` | `实际安装根目录/control.env` |
| Windows 一键安装 | `%ProgramData%/StarhubControl/panel-access.py` | `%ProgramData%/StarhubControl/control.env` |

上面的目录对应不同布局，不是要求创建两套安装。脚本可自动发现默认配置；同时存在多个配置时会拒绝猜测，需使用 `--env` 明确指定。不要把查询命令中的绝对路径写成 `scripts/panel-access.py` 后直接在 `/root` 执行。

## 一键新装 Linux：root 直接执行

查询配置地址、原生网页监听和恢复状态，不显示管理员令牌：

```bash
python3 /opt/starhub-control/panel-access.py status --env /opt/starhub-control/control.env
```

额外开启临时 IP 恢复网页，默认端口 18081，30 分钟自动关闭：

```bash
python3 /opt/starhub-control/panel-access.py enable-ip --env /opt/starhub-control/control.env --allow-http --minutes 30
```

若机器位于 NAT 后、自动识别的地址不是公网 IP，可在这条命令后增加 `--ip YOUR_PUBLIC_IP`；它只指定输出链接中的地址，不配置 NAT、DNS 或防火墙。

关闭临时恢复网页：

```bash
python3 /opt/starhub-control/panel-access.py disable-ip --env /opt/starhub-control/control.env
```

临时恢复默认不随重启启动；确需常开时，在开启命令加 `--persistent`。HTTP 没有加密，开启需要显式 `--allow-http`；系统和云防火墙需允许选定网页端口，脚本不会开放 API / 数据库或修改全局防火墙。

## 旧 /opt/harbor 布局：root 命令

仅在这一布局确实存在时使用，配置仍在 `/etc/harbor`：

```bash
python3 /opt/harbor/panel-access.py status --env /etc/harbor/control.env
```

```bash
python3 /opt/harbor/panel-access.py enable-ip --env /etc/harbor/control.env --allow-http --minutes 30
```

```bash
python3 /opt/harbor/panel-access.py disable-ip --env /etc/harbor/control.env
```

## 脚本不存在时：下载并查询

这个命令只执行 `status`，不打开或关闭入口、不安装面板、不重置令牌。它自动识别 root / sudo，并在默认配置不唯一时停止。自定义目录或多套安装请在两处 `status` 后加 `--env /ABSOLUTE/INSTALL/control.env`。

```bash
(
  set -eu
  if [ "$(id -u)" -ne 0 ] && ! command -v sudo >/dev/null 2>&1; then
    printf '%s\n' '请先执行 su - 切换到 root，再运行本命令。' >&2
    exit 1
  fi
  panel_script=$(mktemp /tmp/starhub-panel-access.XXXXXX)
  trap 'rm -f "$panel_script"' EXIT
  curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/panel-access.py -o "$panel_script"
  if [ "$(id -u)" -eq 0 ]; then
    python3 "$panel_script" status
  else
    sudo python3 "$panel_script" status
  fi
)
```

## 18080、18081 与域名入口的区别

- **普通网页**：一键安装使用 `--public` 时默认监听公网 IP:18080。自定义端口可能不同，查询结果的 `web_listener` 表示实际配置。
- **临时恢复网页**：`enable-ip` 额外创建 `starhub-panel-rescue.service`，默认 IP:18081；查询结果中的 `recovery` 只描述这一服务。
- **域名网页**：Nginx / 宝塔的 80 / 443 域名入口独立于临时恢复服务。

`disable-ip` 只停止临时恢复服务；不会更改普通网页的 `WEB_ADDR`、停止控制面、关闭 Nginx、注销 Agent 或修改订阅。`recovery.active=false` 时仍能访问普通 IP:18080 是正常行为。不要通过卸载面板、停止整个控制服务或删除脚本来关闭一个 IP 入口。

若希望普通网页仅供 Nginx 本机回源，需要在已验证域名入口可用后，单独调整安装配置中的 `WEB_ADDR` 为回环监听，或限制普通网页端口的防火墙访问；这不属于 `disable-ip` 的操作。

`panel_url` 显示已配置的首选入口，`web_listener` 显示原生监听，`recovery.url` 只在恢复服务启用时给出。地址查询不证明公网 DNS、证书、端口或云防火墙已可用。

## 永久关闭普通网页公网监听

这与 `disable-ip` 不同：把普通网页的 `WEB_ADDR` 改成回环监听，外部不能再直接连接 18080；Nginx 在同机通过 `127.0.0.1:18080` 回源，域名继续可用。配置在重启后保留，正常后台更新不会重写 `control.env`。

以下命令只针对一键新装 Linux systemd 的默认 `/opt/starhub-control` 与 18080 端口。自定义前缀、端口或独立旧前端需按实际安装调整；Compose 应调整自身端口发布，不能照抄这一命令。

先确认已绑定的域名可以正常登录；将 `YOUR_PANEL_DOMAIN` 换为自己的面板域名，不含协议、路径和端口。以 root 执行：

```bash
(
  set -eu
  cfg=/opt/starhub-control/control.env
  panel_domain='YOUR_PANEL_DOMAIN'
  test -f "$cfg"
  grep -q '^WEB_ADDR=' "$cfg"

  curl --noproxy '*' \
    --resolve "$panel_domain:443:127.0.0.1" \
    --connect-timeout 5 --max-time 15 \
    -fsS "https://$panel_domain/api/v1/branding" -o /dev/null

  backup="$cfg.before-ip-close.$(date -u +%Y%m%dT%H%M%SZ)"
  cp -p "$cfg" "$backup"
  printf '配置备份：%s\n' "$backup"
  sed -i 's/^WEB_ADDR=.*/WEB_ADDR=127.0.0.1:18080/' "$cfg"
  systemctl restart starhub-control.service
  python3 /opt/starhub-control/panel-access.py disable-ip --env "$cfg"
)
```

此命令会：

- 先通过本机 Nginx 检查指定 HTTPS 域名的面板接口；证书校验或请求失败就停止，不修改现有入口。
- 创建带 UTC 时间的配置备份，保留私有文件权限，只输出备份路径。
- 将普通网页改为仅 IPv4 回环 `127.0.0.1:18080`，关闭该网页端口的 IPv4 / IPv6 公网监听。
- 重启控制端，并停止本项目临时恢复服务。域名网关配置保持原样；控制端重启期间网页 / API 会短暂中断。

检查实际监听：

```bash
ss -lntp | grep -E ':18080|:18081'
```

预期普通网页只监听 `127.0.0.1:18080`，没有 `0.0.0.0:18080` / `[::]:18080`；本项目的临时恢复监听已经停止。其他软件若使用相同端口，应按进程核对。

检查域名接口，然后在浏览器确认仍可登录（替换域名）：

```bash
curl --noproxy '*' --resolve YOUR_PANEL_DOMAIN:443:127.0.0.1 -fsS https://YOUR_PANEL_DOMAIN/api/v1/branding
```

### 恢复本次操作前的普通网页监听

把 `PASTE_BACKUP_PATH` 替换为本次命令输出的实际备份文件路径。不要上传配置文件，其中有管理员令牌和密钥。

```bash
cp -p 'PASTE_BACKUP_PATH' /opt/starhub-control/control.env &&
systemctl restart starhub-control.service
```

这会恢复备份中的普通网页监听范围；临时恢复服务仍通过 `enable-ip` 单独开启。

### 关闭端口与隐藏 IP 的边界

绑定域名本身不能隐藏 IP；DNS 直连记录仍会暴露源站地址。以上命令关闭的是管理网页的直连端口，域名所用的 80/443 仍承接访问。

需要进一步限制绕过 Cloudflare 访问源站时，应使用 Cloudflare 代理，并为源站网页端口单独设置仅接受可信回源的策略；其他直接解析到同一 IP 的 DNS 记录也需要核对。见 [Cloudflare 源站保护说明](https://developers.cloudflare.com/fundamentals/security/protect-your-origin-server/)。这与普通网页的回环监听是两项独立设置。

## SSH 转发和其他安装方式

在自己的电脑执行。新装默认原生端口为 18080：

```bash
ssh -N -L 18082:127.0.0.1:18080 YOUR_USER@YOUR_VPS
# 本机浏览器打开 http://127.0.0.1:18082
```

旧版独立前端可能使用 5173；按实际 `web_listener` 调整目标端口。若监听为 `0.0.0.0` 或 `[::]`，SSH 转发连接目标使用对应回环地址，而不是把通配监听地址照抄为远端目标。

Windows PowerShell 查询：

```powershell
python "$env:ProgramData/StarhubControl/panel-access.py" status --env "$env:ProgramData/StarhubControl/control.env"
```

Windows / 自行监督的进程可在查询命令中把 `status` 换为 `enable-ip --allow-http --foreground`；保留窗口，超时或 Ctrl+C 结束，不使用 Linux systemd 的 `disable-ip`。Compose 不使用宿主一键安装目录；通过项目自己的端口映射或 SSH 转发访问，见 [安装文档](INSTALL.md)。

## 常见报错

| 输出 | 原因与处理 |
| --- | --- |
| `sudo: command not found` | root 直接去掉 sudo；普通用户先 `su -`，不必为 root 安装 sudo。 |
| `can't open file '/opt/harbor/panel-access.py'` | 在新安装上使用了旧路径；改用 `/opt/starhub-control/panel-access.py`。 |
| `Choose one installed control.env with --env` | 未找到唯一的默认配置；使用实际的绝对路径明确指定配置，不要新建空配置。 |
| `Recovery entrance disabled` 后仍可打开 IP:18080 | 只关闭了临时 18081，普通网页仍按原配置运行。 |
| `Address already in use` | 备用端口被占用；指定其他未占用的 `--port`，不要停止未知服务。 |

安装、卸载及管理员令牌命令见 [README](../README.md)。域名与证书依赖修复见 [网关文档](GATEWAY.md)。
