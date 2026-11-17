# 星枢 0.25.9 · 协议参数样式、推荐值与客户端自动适配

新增协议和编辑协议的“握手与封装参数”共用既有协议设置区块的边框、圆角、背景、标题与移动端间距，修复组件未加载局部样式的问题。批量添加协议也展示同一设置区块。

## 推荐参数

| 适用设置 | 新建推荐值 | 简要说明 |
| --- | --- | --- |
| 普通 TCP/TLS | Chrome 指纹 | 采用已验证的常用指纹；证书验证保持开启。 |
| REALITY | 原有 Chrome 指纹 | 沿用专用字段和内核兼容限制。 |
| XHTTP | auto、100–1000 字节填充、保留 gRPC 伪装头 | 由内核选择模式，避免强制不适合 CDN 的流式方式。 |
| gRPC | multiMode 关闭 | 优先支持常用客户端；可手动开启。 |
| mKCP | none 包头、MTU 1350、TTI 50 ms、上行 5 / 下行 20 MB/s | 使用所固定 Xray 版本的基础值，Seed 留空、拥塞控制关闭；不虚设线路带宽。 |
| QUIC / Naive | 协议自身握手 | 不添加不适用的 TCP/uTLS 指纹。 |

推荐值适用于常见场景，不保证对每条线路速度最优。新建和切换协议 / 承载时自动选择；打开已有配置、导入 JSON 时保留已存值，可按“应用推荐参数”主动替换。原有 HY2 / TUIC / AnyTLS 专属调优保留。

## 客户端自动处理

- 完整 Xray JSON 保留手动开启的 gRPC multiMode；其他格式自动导出同一服务端支持的普通 gRPC，保留服务名、SNI、认证和 TLS。
- HTTP CONNECT 浏览器指纹在支持的 sing-box / Xray 完整 JSON 中保留。Mihomo、Clash Verge 等没有该能力的适配自动使用客户端原生 TLS，不再仅因可选指纹而过滤整条 HTTP 节点。
- 处理只作用于订阅副本，不改写服务器配置、不轮换用户凭据，不关闭证书验证。订阅检查显示自动适配的入口数量及说明。
- XHTTP、mKCP 等实际承载无法通过删除参数变为另一种协议；当前未适配该承载的客户端仍自动过滤对应节点，并显示过滤数量。REALITY 公钥、Short ID、安全层和必要封装不会被删减。

## 验证范围

前端推荐值与旧配置 / JSON 保留测试、生产构建、浏览器表单视觉检查、全部客户端目录的订阅筛选、HTTP 指纹兼容导出、订阅检查提示、数据库身份保留回归，以及普通 gRPC 回退的真实 Xray 转发。正式分发继续要求同提交的跨平台 CI 与完整安装、升级回滚、Agent 接入及卸载验证成功。

无数据库迁移。不使用新增封装的旧 Agent 可继续运行；使用 0.25.7 已引入的服务端传输参数或链式参数仍要求 0.25.7+，推荐更新到本次正式版。

参考：[固定版本 Xray mKCP 默认值](https://github.com/XTLS/Xray-core/blob/v26.10.21/transport/internet/kcp/config.go)、[XHTTP](https://github.com/XTLS/Xray-core/discussions/4113)、[gRPC](https://xtls.github.io/config/transports/grpc.html)。
