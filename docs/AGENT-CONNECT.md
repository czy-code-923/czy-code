# 一键连接其他 VPS

这台已安装控制台、数据库的 VPS 是总管理端。其他主机仅运行一个主动连接的 Agent，sing-box/FRP 实例按需启动，不安装数据库、不需要 Docker，也不需要向公网开放 Agent 管理端口。

## 面板操作

1. 主机资产 → 新建主机，只需填写名称；供应商和地址可选，无公网 IPv4 时地址可留空。
2. 创建后自动打开“连接主机”，复制一键安装命令。
3. 在目标 VPS SSH 终端执行，按终端提示等待安装完成；普通用户会通过 sudo 提权安装，常驻 Agent 使用受限系统用户。
4. 面板通过实际 Agent 心跳显示已连接，并展示 Agent 版本、操作系统/架构及自动上报的接口地址。手工填写的资产地址不会被覆盖。
5. 然后在“域名与分享”中为目标主机创建真实代理、FRP 或中转服务。接入 Agent 本身不会自动发布代理端口。

当前一键命令支持 Linux amd64/arm64 + systemd（例如 Debian/Ubuntu/RHEL 系列），需 curl、Python 3、有效 CA 证书、到控制域名的 HTTPS 出站连接以及常见 useradd/systemctl 工具。Alpine/OpenRC、Windows 的一键向导尚未提供，现有手动方式见 INSTALL.md。不要在宿主总管理 VPS 上重复运行新主机安装命令；已有 Agent 会被保护并拒绝覆盖。

## 注册与安全

- 管理员令牌仅用于面板生成命令，不会传到其他主机。
- 注册命令中的随机临时凭证只有 15 分钟有效，只能消费一次，数据库只保存其哈希。
- 重新生成命令撤销未消费的旧命令。新命令成功注册之前，不会中断现有 Agent 凭据；成功注册会替换该主机的 Agent 凭据。
- 安装包和脚本通过验证证书的 HTTPS 下载；拒绝跳转，按 SHA-256/大小校验安装包，只提取固定五个二进制文件。
- 安装前检查已有目录/服务。首次安装失败清理本次创建的文件；如凭证已消费，请修复终端提示后重新生成命令。
- 命令含短期凭证，不要公开分享。已复制命令可在向导撤销，关闭向导不撤销，便于复制后去另一台服务器操作。
- 真正的长效 Agent 凭据仅写入目标机 root 0600 环境文件。服务没有 root 权限、网络管理 capability 或任意 shell 接口。
- 向导的安装完成提示与连接成功分开：安装器启动服务后，仍需面板收到心跳才算连接成功。

## 总管理端发布安装包

在源码根目录执行 `python scripts/build-agent-releases.py`，需 Go 和到官方 GitHub 发布页的网络。工具编译两种架构 Agent、下载并校验固定版本引擎，生成 `agent-releases/`。Compose 已只读挂载该目录，重建新版控制端即可使用。原生部署将生成目录复制到受信任版本目录后设置下面的环境变量。更新时先生成新目录，避免覆盖运行中的发布包。

将受信任发布目录设为 `AGENT_RELEASE_DIR`，例如 `/opt/harbor/agent-releases-v05`。每个版本目录保持 root 所有且普通用户不可写：

```text
manifest.json
linux-amd64.zip
linux-arm64.zip
```

ZIP 内只包含 `harbor-agent`、`sing-box`、`frps`、`frpc`、`xray` 五个经过验证的二进制。manifest 对应各平台的 `sha256` 和 `size`。固定路径下载接口不会接受任意文件路径；下载并发最多 4 个，注册请求有固定内存限速。未配置发布目录时，面板会报告不可用，不会生成一条假安装命令。

API：管理员 `POST/GET /api/v1/hosts/{id}/enrollment`、`POST .../enrollment/revoke`；公开引导 `POST /api/v1/agent/enroll`。注册成功后才获得仅限该主机的 Agent 凭据，不能读取其他主机或管理 API。
