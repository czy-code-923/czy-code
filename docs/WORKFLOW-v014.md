# 0.14 客户端配置工作区

## 四个独立分区

基础配置、DNS、策略组、规则分别编辑，存为 `basic`、`dns`、`proxy-groups`、`rules`。这是一份不含节点身份的通用模板源，不是直接运行的 Mihomo 配置。分享时基础字段展开为 Mihomo 顶层，其他客户端按内核能力编译。

策略组与规则位于同页，桌面左右显示、手机纵向显示。每组一张卡片，每条规则单独编辑、创建、删除、排序。规则每页 25 条并可搜索；MATCH 保持末尾。组改名同步更新其他组、规则和 DNS 的引用；仍被引用的组不能直接删除。未应用分区 JSON 或单条编辑会阻止保存，防止丢失修改。

默认模板基于用户提供的 `mihomo_max.yaml`：21 组、186 条规则，保留服务分类与顺序。文件内节点密码、UUID、服务器定义不进入模板。具体节点别名替换为协议或 IPv4/IPv6 动态引用，只匹配订阅用户已获授权且运行中的节点。

默认关闭客户端 LAN 共享和 TUN，DNS 和控制器使用回环地址。respect-rules 配置下 prefer-h3 关闭。需要时可以在基础分区手动调整客户端 LAN/TUN 设置，管理控制器仍限回环。首次安装自动初始化一次全局模板；升级保留已有设置，之后删除默认策略不会在重启时重建。面板“使用文件模板设为全局默认”载入草稿，保存后生效。

## 导入与手写

支持单个 YAML/JSON 配置（128 KiB），识别 Mihomo 完整配置、通用模板、sing-box selector/urltest 与路由、Xray 出站/balancer/路由，以及 Shadowrocket INI 规则配置。不会拉取文件中的远程提供者，不读取其中的本地路径，不执行程序。导入只生成草稿，不修改主机。

返回识别类型、各分区、节点映射建议、未匹配引用和字段差异。节点别名优先建议同名稳定 ID，其次动态协议；应用前确认映射。只导入选择的分区。原生 DNS/日志覆盖默认不导入，需要时勾选并核对旧出站标签。JSON 无策略组时从 selector/balancer 提取分组；原生节点身份一律丢弃。

无法转换的规则不能通过“确认”跳过后应用到通用规则分区，必须改写原配置或取消该分区。特别是多字段 AND 不能拆成多条 OR。未知 Shadowrocket 规则集可保留为该客户端原生覆盖，但不宣称可以转换为其他内核规则。

基础和 DNS 提供表单及独立 JSON 编辑；完整源使用通用 YAML/JSON。每个客户端还可通过 `native_options`（mihomo / sing-box / xray / shadowrocket-config）手动覆盖。订阅显式绑定的客户端模板最后生效；模板本地端口优先于通用端口。节点列表和身份始终由权限模型生成。

## 跨客户端行为

- Mihomo 保留原生组、规则、DNS 和基础设置。
- sing-box 转为 selector/urltest、route 与新 DNS server。引导、节点解析、直连和主 DNS 分开映射；支持 Fake IP 和排除域名。Geo 规则转 SagerNet 官方 SRS，客户端首次使用需通过 proxy 出站下载数据。含 REJECT 的交互组按默认首项转换并提示，原生客户端不提供同样的拒绝选项切换。
- Xray 无 Mihomo 式交互选择，select 固定首项、url-test 使用 leastPing/balancer。Geo 规则使用 Xray 原生 Geo 数据，客户端需要对应 geoip.dat/geosite.dat。no-resolve 映射为全局 AsIs 并提示：IP 规则不主动解析域名。DNS 使用真实 IP，Mihomo fake-ip/TUN 使用客户端自身设置。
- Shadowrocket 分两份链接：节点订阅、分流配置。分流配置为 `[General]` / `[Rule]`，代理目标折叠为应用的 PROXY，直连/拒绝目标保留，不伪造 JSON selector。导入节点后再导入分流配置，并在应用中选择代理。CN/Telegram GeoSite 使用对应 Shadowrocket 规则集；数据来源和内置 Geo 数据可能不同，预览会说明。其他 GeoSite 需要专用设置。
- 节点 URI 和 NaiveProxy 单节点配置无法携带这些规则，继续使用明确的仅节点模式。

不同客户端的 Geo 数据、fallback-filter、nameserver-policy、TUN/嗅探、测速与 DNS 并发并不等价。预览列出已转换与需要原生设置的部分；不能只把 YAML 后缀改成 JSON。未授权或停用节点不会因导入和动态选择而进入订阅。

## API

- `GET /api/v1/client-policies/default`：无身份的默认模板。
- `POST /api/v1/client-policies/import`：`{source}`，返回分区、映射和差异，不落库。
- `POST /api/v1/client-policies/parse`：通用源与结构化分区往返。
- `POST /api/v1/client-policies/preview`：`{source,client_id,native_options}`，只返回不可用的节点占位及编译差异，无真实凭据。
- 现有策略增删改 API 继续使用版本锁，订阅与用户/客户端优先级保持一致。使用 templates:read/write 权限；完整字段见 OpenAPI。

本地及 CI 验证覆盖提供的默认模板、三种原生内核配置、原生/INI 识别、字段差异、跨区改名、权限与凭据剥离、单条编辑、分页和未应用修改保护。Shadowrocket 在本环境没有 iOS 运行端；分流文件按独立规则配置生成，不能声称完成 iOS 真机连接验证。

0.14.1 修正工作区页签样式与内边距，增加编辑表单的窄屏间距；配置结构和转换逻辑保持一致。
