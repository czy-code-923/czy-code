# 星枢 API 与外部集成（0.11）

服务端保持 `/api/v1/` REST API。在 **系统设置 → API 与集成** 创建密钥，选择功能、到期时间和每分钟请求数。密钥明文只显示一次，数据库只存哈希；修改权限和撤销在下一次请求立即校验。创建/修改/撤销写入审计，使用密钥修改业务资源时审计 actor 为 `api-key:<id>`。

当前是单一自用工作区 `self`。功能权限覆盖该工作区全部资源，**没有按主机、按用户的资源隔离，也不应直接作为面向客户的登录系统**。完整管理员令牌负责密钥管理及网关特权工作，不能交给机器人。Telegram 集成仅预留 API，本项目不自动发送任何 Telegram 消息。

## 调用

将密钥保存在机器人/独立服务的环境变量或秘密存储中，不放入源码、日志、URL 或公开网页。

```bash
export STARHUB_URL=https://manager.example.com
# 在当前安全终端中设置 STARHUB_API_KEY，示例不包含真实密钥。
curl --fail --silent --show-error \
  -H "Authorization: Bearer $STARHUB_API_KEY" "$STARHUB_URL/api/v1/session"
curl --fail --silent --show-error \
  -H "Authorization: Bearer $STARHUB_API_KEY" "$STARHUB_URL/api/v1/proxy-users"
```

- `GET /session`：当前身份与权限；`GET /api-capabilities`：功能/权限目录。
- `GET /openapi`：嵌入服务端的 OpenAPI 3.0 文档，源文件为 [openapi.json](openapi.json)。
- `GET /proxy-users`：用量、分配额度、剩余量、下次重置；需要 `users:read`。
- `POST /proxy-users/{id}/reset-traffic`：`{"restart_cycle":false}`；需要 `usage:reset`。清除本期额度，保留累计量与 Agent 计数基线；设为 true 同时重启按月周期。
- `GET /subscriptions/{id}/usage`：用户或共享入口用量诊断；需要 `subscriptions:read`。
- `GET /subscriptions/{id}/link`：恢复链接；`GET /subscriptions/{id}/config?format=mihomo`：已隐藏凭据的最终配置预览。都需要单独的 `subscriptions:links` 权限。
- `POST /live/batch`：一个主机/内核最多 8 种协议，`port:0` 自动分配；必须携带 32 位十六进制 `request_key`。同一请求体重试不重复创建，保留 30 天。需要 `nodes:write`。
- `PUT /client-profiles/{id}`：携带读取到的 `version`、格式、端口、策略和 `extra_text`。支持 JSON/YAML，最多 32 KiB，版本冲突返回 409。
- `PUT /subscriptions/{id}`：可绑定用户或修改共享协议集合、关联模板，保持链接令牌；改变绑定必须携带版本。用户入口来源跟随用户组。
- `401` 密钥无效/过期/撤销；`403` 权限不足；`409` 版本冲突；`429` 超过每分钟上限。限流为单控制面进程内存窗口，重启后窗口重新计数。

## 功能权限

| 领域 | 权限 |
|---|---|
| 主机 | `hosts:read`、`hosts:write`、`hosts:enroll`（含接入凭据） |
| 协议 / 内核 | `nodes:read`、`nodes:write` |
| 用户 / 用户组 | `users:read/write`、`groups:read/write` |
| 流量 / 访问 | `usage:read`、`usage:reset`、`access:read`（来源 IP/目标） |
| 订阅 | `subscriptions:read/write`、`subscriptions:links`（敏感凭据） |
| 模板 | `profiles:read/write` |
| 站点 | `site:read/write` |
| 证书 / 反代 / 防火墙 | `certificates:read/write`、`reverse:read/write`、`firewall:read/write` |
| 高级库存 / 任务 / 审计 | `inventory:read/write`、`tasks:read/write`、`audit:read` |

表中 `read/write` 表示两个分别可选的权限，不是字面量。创建主机会返回 Agent 凭据，因此同时要求 `hosts:write` 与 `hosts:enroll`；创建/轮换订阅会返回链接，因此同时要求 `subscriptions:write` 与 `subscriptions:links`。

## 独立前端

在 **站点与域名 → 独立前端允许来源** 中逐行填写准确来源，例如 `https://panel.example.com`。最多 8 个，不支持通配符、URL 路径或任意公网 HTTP 来源；开发环境允许回环 HTTP。CORS 只允许浏览器跨域请求，不替代 Bearer 权限。每次请求由设置白名单校验，不信任客户端伪造的转发头。API、数据库、Agent 管理端口继续只监听回环或主动连出。

## 模板与流量约定

协议参数在 **节点配置 → 详情/编辑 → 手动编辑协议参数 JSON** 修改，保存后生成完整内核版本，Agent 原生检查成功才应用。不能直接覆盖自动合并的用户身份或统计端口。

客户端参数在 **订阅分享 → 客户端配置模板** 编辑 DNS、规则、日志等，按模板格式合并；切换为其他导出格式不会错误套用原格式的参数。生成的节点和凭据由控制面维护。客户端自定义入站及管理接口须显式监听回环。模板进行结构与容量校验；复杂路由仍需在实际客户端验证。

Clash/Mihomo 输出原生 YAML。无格式参数的链接优先使用关联模板，否则依据 Clash/Mihomo User-Agent 选择 YAML。响应头始终带 `upload`、`download`、`total`（字节）和 `expire`。个人订阅统计独立用户；共享订阅统计所选入口累计，额度为 0 表示未设置个人额度，不能假装是某个用户用量。额度耗尽仍可刷新用量，返回拒绝代理的配置。Clash 的卡片用量在更新订阅时刷新，并非实时测速。

按月周期从设置时刻或指定起点计算，北京时间，保留原始日期；1 月 31 日 → 2 月最后一天 → 3 月 31 日。控制面每 5 秒检查，不依赖浏览器或 Agent 在线。仅清除本期用量，累计量与上报去重基线保留；离线期间无法精确切分的连接字节在下次收到上报时归入当期，不宣称逐字节跨周期账单。

参考：[Clash Verge 响应头](https://www.clashverge.dev/guide/url_schemes.html)、[Remnawave 用户与流量](https://docs.rw/learn-en/users/)、[sing-box 路由](https://sing-box.sagernet.org/configuration/route/)。
