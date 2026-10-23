# v0.6 协议与模板

## 真实执行模板

|协议模板|服务端引擎|传输|证书|订阅|
|---|---|---|---|---|
|Shadowsocks 2022 AES-256-GCM|sing-box|TCP/UDP|不需要|sing-box / Mihomo / Shadowrocket URI|
|VLESS TCP TLS|sing-box 或 Xray|TCP|公开受信任完整链|同上|
|Trojan TCP TLS|sing-box 或 Xray|TCP|公开受信任完整链|同上|
|Hysteria2|sing-box|QUIC / UDP|公开受信任完整链|同上|
|TUIC v5|sing-box|QUIC / UDP|公开受信任完整链|同上|
|Snell v4 兼容|sing-box v5 入站|TCP / UDP|不需要|sing-box / Mihomo；URI 列表返回422|

模板由受限字段生成，不接受任意命令、任意文件路径或完整引擎配置。GET /api/v1/protocol-templates 返回协议目录。VLESS 和 Trojan 可明确选择运行引擎；其余模板拒绝不支持的引擎组合。固定版本为 sing-box 1.14.3、Xray 26.10.21、FRP 0.71.1，下载验证官方发布 SHA-256。

## 面板操作

1. 先安装或升级目标主机至 Agent 0.6.0；旧 Agent 不接收新协议任务，创建和启用均有版本保护。
2. 在域名与分享的新建真实服务表单中选协议模板、运行主机、地址和端口；VLESS/Trojan 另选引擎。
3. TLS 模板填写实际服务器名及对应的 fullchain PEM、私钥 PEM。控制面检查完整信任链、主机名、时间及私钥匹配后加密存储；Agent 将配置与私钥写入受限状态目录。客户端证书验证始终开启。
4. 创建后默认停用。放行对应的 TCP / UDP 业务端口后再启用；HY2/TUIC 不能只放行 TCP。Agent 原生检查通过后才启动候选进程。
5. 勾选服务创建订阅，再手动选 sing-box、Mihomo 或 Shadowrocket 格式。仅下发新鲜回报运行的服务，不把私钥发给客户端。
6. 证书续期或更换代理凭据时创建替代服务，验证后勾选新服务，对已有订阅点击“替换为所选节点”；分享链接和有效期保持不变。最后停止旧服务。

节点证书目前需要自行签发和续期；总管理端域名证书同步不等于其他主机证书管理。暂未提供 REALITY、Vision、WebSocket/gRPC/XHTTP、多用户套餐或任意自定义 JSON 模板。不得把这些基础模板称为所有传输组合均已实现。

## 兼容性与运行边界

Snell 服务端使用 sing-box 的 version=5，客户端使用兼容的 version=4；当前不启用 Snell v5 QUIC 模式。Snell 缺少统一 URI 分享标准，因此不伪造 Shadowrocket URI，也不会悄悄从订阅剔除节点。

Xray 保留新版 Freedom 默认保护：私有/保留目标地址可能被拒绝。公网代理与内网访问不是同一用途。FRP 仍用于明确的内网穿透；不要为了本地测试全局关闭 Xray 保护。

每个启用实例会启动一个引擎子进程；未启用实例不启动。现有 Agent 内存和进程总量限制继续生效，建议先少量启用并观察宿主内存，不能按64条数据上限推断小内存VPS能同时运行64个实例。

## 验证

TestProtocolTemplates 对五种 sing-box 模板及两种 Xray 模板逐一执行官方原生配置校验，再启动服务端与 sing-box 客户端，使用本地 HTTP 目标验证真实转发。Xray 测试仅给该测试目标的确切地址和端口添加例外，发布模板没有该例外。另验证不可信证书、主机名不匹配、错误私钥和未知引擎拒绝。

Windows 本地运行示例：

```powershell
$env:TEST_SING_BOX_BINARY=(Resolve-Path engines/sing-box.exe).Path
$env:TEST_XRAY_BINARY=(Resolve-Path engines/xray.exe).Path
go test -count=1 -run TestProtocol -v ./internal/live
```

这不替代公网防火墙/运营商 UDP 可达性验证，也不等于已经在每种真实手机客户端完成导入测试。

官方依据：
- https://sing-box.sagernet.org/configuration/inbound/snell/
- https://sing-box.sagernet.org/configuration/inbound/vless/
- https://sing-box.sagernet.org/configuration/inbound/hysteria2/
- https://sing-box.sagernet.org/configuration/inbound/tuic/
- https://xtls.github.io/en/config/inbounds/vless.html
- https://xtls.github.io/en/config/outbounds/freedom.html
- https://wiki.metacubex.one/en/config/proxies/snell/
