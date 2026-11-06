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

新增 `disable-public` 操作针对 Linux systemd 一键安装。它从私有配置和本机鉴权 API 自动读取实际域名、HTTPS 设置与网页端口，验证域名回源后将普通网页改为回环监听。自定义前缀用实际 `--env` 路径；旧 `/opt/harbor` 与 Compose 使用各自的服务布局，不照抄此操作。

下面是一条完整命令，以 root 执行，无须 sudo。复制整行，终端视觉换行不影响；不要在命令中手动加入空行或反斜杠续行，也不用替换域名：

```bash
( set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/panel-access.py -o "$f"; python3 "$f" disable-public --env /opt/starhub-control/control.env )
```

维护流程：

- 验证私有配置、二进制和 systemd 主服务归属；与更新助手使用同一维护锁，避免打断备份、更新或回滚。
- 从已保存站点读取有效 HTTPS 域名，经过本机 Nginx 检查证书及返回的面板版本；未配置域名或检查失败时不修改现有入口。
- 创建仅本机保存的配置备份，只更改 `WEB_ADDR`，保留管理员令牌、数据库凭据和密钥；实际网页端口保持不变。
- 重启控制端，复查健康和域名访问，再停止本项目临时恢复服务。应用阶段失败时恢复普通网页的原配置并重启；回退仍异常时明确给出私有备份路径。

成功结果会显示 `web_listener`、`domain`、`backup` 和 `recovery_disabled`。默认目标是 `127.0.0.1:18080`，IPv4 / IPv6 公网不再监听这一普通网页端口。已是回环监听时返回 `changed=false`，不重复创建备份。不要上传备份文件，其中含有私有凭据。

此操作会短暂重启控制端。Nginx 域名网关继续本机回源，重启和正常后台更新保留 `control.env`。检查默认端口：

```bash
ss -lntp | grep -E ':18080|:18081'
```

预期只有 `127.0.0.1:18080`，没有 `0.0.0.0:18080`、`[::]:18080` 或本项目的临时恢复监听；自定义端口按结果中的实际端口检查。

### 恢复本次操作前的普通网页监听

把 `PASTE_BACKUP_PATH` 替换为本次结果中的实际 `backup` 路径：

```bash
cp -p 'PASTE_BACKUP_PATH' /opt/starhub-control/control.env && systemctl restart starhub-control.service
```

仅用于恢复本次普通网页配置；若备份后另改过凭据或其他配置，应按实际变更恢复，避免覆盖新设置。临时恢复服务仍通过 `enable-ip` 单独开启。

### 复制报错与脚本版本

- `curl: (2) no URL specified`：反斜杠续行后出现了空行，curl 提前执行。旧命令的 `set -e` 会在此处停止，后面的关闭操作并未执行。
- `YOUR_PANEL_DOMAIN` 是旧示例的占位符，不是真实域名；新的 `disable-public` 自动读取已绑定域名。
- 本机旧版脚本若显示 `invalid choice: disable-public`，使用上面的下载执行命令即可；这会运行更新后的维护脚本，不要求重装面板。
- HTTPS 检查失败会保留原配置；先核实已保存域名、证书、本机网关及其回源，不要用关闭证书校验的方式跳过检查。

### 关闭端口与隐藏 IP 的边界

绑定域名本身不能隐藏 IP，DNS 直连记录仍暴露源站地址。以上操作关闭管理网页的直接端口，域名所用的 80/443 仍承接访问。

进一步限制绕过 Cloudflare 的请求，需要使用 Cloudflare 代理并为源站网页端口单独配置可信回源策略，同时核对其他 DNS 记录是否暴露同一 IP。见 [Cloudflare 源站保护说明](https://developers.cloudflare.com/fundamentals/security/protect-your-origin-server/)。这与普通网页回环监听是两项独立设置。

## 后台常驻与开机自启

关闭 SSH 窗口不会停止 systemd 管理的面板。新装 Linux 已安装主服务；需要统一启用已安装组件时，以 root 运行下面的完整单行命令：

```bash
( set -eu; f=$(mktemp); trap 'rm -f "$f"' EXIT; curl --proto '=https' --tlsv1.2 -fsSL https://raw.githubusercontent.com/czy-code-923/czy-code/main/scripts/panel-access.py -o "$f"; python3 "$f" enable-autostart --env /opt/starhub-control/control.env )
```

`enable-autostart` 会验证安装目录、配置、主服务、数据库容器和数据卷归属，并执行：

1. 启用、启动 `docker.service`。
2. 校验本项目 `starhub-control-db` 的镜像、数据库身份、私有端口和数据卷；将没有自动重启策略的容器设为 `unless-stopped`，保留已有 `always`，并启动已停止的项目数据库。
3. 启用、启动 `starhub-control.service`，等待控制端健康。
4. 若网关明确使用匹配的系统 `/usr/sbin/nginx`，启用 `nginx.service`；宝塔或外部 Nginx 继续由原软件管理。
5. 验证并启用已安装的 `starhub-gateway.timer` 和 `starhub-panel-updater.timer`，不把 oneshot 服务改为常驻，也不重新开启恢复入口。

`unless-stopped` 会在正常重启后恢复运行中的容器；以后若主动停止容器，仍尊重这次停止，需再次启动。已有的 `always` 策略不降级。

结果中的 `enabled` 列出已启用的服务 / 定时器，`missing_helpers` 列出未安装的助手。若 `complete=false`，表示当前安装缺少助手，应按 [网关修复](GATEWAY.md) 或 [更新助手安装](RELEASE-v0238.md) 补齐；自启命令不会伪造或安装缺失服务。现有助手文件不完整或归属异常会停止操作。其他 VPS 的 Agent 继续由各自的安装服务启动。

检查：

```bash
systemctl is-enabled docker.service starhub-control.service starhub-gateway.timer starhub-panel-updater.timer
systemctl status starhub-control.service starhub-gateway.timer starhub-panel-updater.timer --no-pager
docker inspect --format '{{.HostConfig.RestartPolicy.Name}}' starhub-control-db
```

已安装的组件应显示 `enabled`，主服务 / timer 应为 `active`。网关和更新助手的 `.service` 是按需执行的 oneshot，显示 `static` / 暂时 `inactive` 不表示未设置自启；应查看对应 timer：

```bash
systemctl list-timers --all --no-pager | grep -E 'starhub-gateway|starhub-panel-updater'
```

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
