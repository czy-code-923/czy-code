# v0.6 部署验收（2026-10-23）

总管理端 https://vps.iczy.top 已升级，管理主机 Agent 回报 0.6.0。公开安装入口包含 harbor-agent、sing-box、frps、frpc、xray 五个经过 SHA-256 校验的二进制；Linux amd64/arm64 包均已配置。其他主机需要先升级 Agent，才可启用新增协议。

升级前在独立新建 PostgreSQL 数据库运行完整 API 集成测试，通过后清理测试数据库；生产数据库未用作测试库。Linux 普通用户下真实运行 7 组协议转发：sing-box VLESS、Hysteria2、TUIC、Trojan、Snell，以及 Xray VLESS、Trojan，全部通过。Windows 官方引擎相同 7 组测试通过。前端 20 项测试和生产构建通过；一键安装器 3 项隔离测试通过。

部署前运行已有数据库备份服务，二进制、私有环境文件和网页备份保存在服务器 /var/backups/harbor/protocols-20261023T130518，目录权限 0700。控制端、Agent 升级成功后才发布前端静态文件。升级脚本包含失败回滚。

外部验收确认 HTTPS v0.6 登录页可访问，一键安装脚本已提供 Xray，未登录管理 API 返回 401。保留只监听回环地址的 API 和数据库端口，以及 Agent 普通用户、资源限制、固定引擎命令和证书验证。

这次没有连接额外 VPS、没有自动开启新公网代理端口，也未在真实 Mihomo/Shadowrocket 手机端逐一导入验证。节点证书续期仍为手动；Snell 不输出 Shadowrocket URI；REALITY/Vision/WS/gRPC/XHTTP 不属于本次模板。完整协议范围和操作见 [协议文档](PROTOCOLS-v06.md)。
