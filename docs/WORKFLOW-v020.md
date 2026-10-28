# 星枢 0.20：客户端识别与订阅分享

1. 在用户页选择组权限，点击「分享订阅」打开原有有效的通用用户链接，没有链接才生成。订阅分享导航只展示已有链接和用户选择，不自动弹出新建；高级选项保留额外订阅与历史配置。
2. 用户默认兼容模式。Clash Verge / FlClash 普通请求无需 HWID，可直接导入；不限名额也记录客户端 UA、来源 IP、最近订阅更新时间。用户下「客户端」查看、改名、停用、删除记录。订阅更新后展示，按可见页面每 30 秒刷新。
3. 兼容模式按来源 IP + 客户端输出家族（有合法 X-Hwid 则按客户端自报标识）检查最近 7 天启用的订阅访问名额。相同 IP / 格式会合并，换网络可能占新名额。限制的是订阅下载名额，不能证明物理设备数、限制同时在线硬件或阻止复制已导入凭据；停用记录只停止后续下载，已有凭据须停用用户 / 轮换凭据撤销。用户与组取最严格非零上限；降低上限前须停用多余记录。单用户最多 200、工作区最多 10,000 条，自动清理 7 天前记录。
4. 严格 HWID 模式保留独立身份，要求客户端回传 X-Hwid；Clash Verge / FlClash 普通订阅不支持此模式。首次身份或新增授权等待 Agent 应用时返回稍后重试提示。HWID、系统型号仍是客户端自报，可伪造。普通客户端没提供的型号不虚构；兼容记录的流量归入用户总量，代理活动时间仅按相同来源近似关联。
5. 格式默认自动识别。主列表整合 Clash/Mihomo（Clash Verge、FlClash）、Shadowrocket 节点、sing-box、Xray、Happ、v2RayTun。高级列表保留各应用别名、通用 Base64、Naive 和 Shadowrocket 分流配置。小火箭节点列表不带分流规则；分流配置是独立高级文件，需要配套节点订阅。手动和自动 Shadowrocket 节点输出一致，不再因已有策略返回 422。
6. 节点权限、IPv4 / IPv6 选择仍由用户组与客户端策略动态决定。原链接更新订阅后获取变化，服务端身份必须先由 Agent 应用。兼容登记不创建额外身份、不重启代理。

## API 与反代

用户新增/编辑字段 `device_limit_mode: compatible | hwid`，默认 compatible。`GET /proxy-users/{id}/devices` 合并真实收到的兼容访问记录和旧 HWID 身份；兼容记录 `registration_source=observed_client`、`enforcement=subscription_downloads`，不包含指纹和密钥。PUT/DELETE 原设备路径编辑或删除记录；兼容记录不接受节点权限。管理员 check/config 预览不登记占位。

`POST /proxy-users/{id}/subscription` 请求 `{}`，返回敏感 url、id、reused；需要 users:read、subscriptions:write、subscriptions:links 三个权限。已有有效的通用链接复用，旧链接与设备链接不自动撤销。

只有回环反代可声明 X-Real-IP；公网直连忽略伪造头。项目内网页代理覆盖公网客户端来源，Nginx 订阅入口必须 `proxy_set_header X-Real-IP $remote_addr;`，CDN 场景只会看到 CDN 地址，需自行设可信边缘来源；不要任意信任客户端 X-Forwarded-For。保留 7 天来源地址属于连接元数据，不记录网页正文。

## 安装、卸载与令牌

[README](../README.md) 前部提供 Linux / Windows 一键部署、一键卸载、星枢面板 ADMIN_TOKEN 读取与源码/Compose 方式。令牌读取不会访问宝塔 API，也不会更改密钥。默认卸载保留数据；完整数据清理使用显式参数。
