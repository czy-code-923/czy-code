# 域名、订阅、NAT与IPv6

## 域名与网关

PUT /api/v1/site保存{domain,https}；GET另返回gateway_domain、gateway_https、gateway_state、gateway_checked_at。面板可修改域名及HTTPS；安装域名同步器后自动处理网关。DNS由域名所有者提前设置，平台不保存DNS账户密钥。

已回报生效仅表示本地证书检查和nginx重载成功，不是公网探测。关闭HTTPS要在本地同步器明确使用--allow-http，面板显示明文凭据风险。

## 真实服务

POST /api/v1/live创建默认停用、不可变服务，类型shadowsocks/frps/frpc。host_id指定设备，IPv6不加方括号，listen只接受127.0.0.1、::1、0.0.0.0、::。与库存实例和FRP映射端口交叉预留，操作系统端口仍需现场检查。

- Shadowsocks AEAD-2022随机32字节密码，next_id引用既有服务形成最多8跳无环链，先建出口再建入口。
- FRP服务端指定可达IPv4/IPv6/域名及1–64条TCP映射；默认回环监听，公网监听须主动选择。
- FRP客户端归属另一主机，parent_id关联中继。默认继承全部映射，也可选子集并改客户端回环目标。相同服务端远端端口只能分配一个客户端，多客户端使用不相交映射。
- FRP强制TLS，专用证书带SAN，客户端固定信任并验证服务端地址，不跳过证书验证。服务端私钥不下发客户端。专用证书有效期一年，过期前通过新服务端/客户端迁移；它与网关ACME证书不同，尚无面板一键续签此专用证书。

toggle接口控制期望启停。Agent每10秒同步，固定引擎check/verify后启动，退出退避重试；45秒内版本相符回报才标记运行新鲜。控制面失联90秒停止进程。进程运行不等于FRP映射或公网访问验证成功。

真实服务配置暂不可原位编辑，升级/换密钥采用停用与新建替代；历史模拟/暂存任务继续保留，不能声称真实服务已经具备完整版本回滚。

## 订阅

创建接口接收名称、1–365天、1–64个Shadowsocks服务ID，返回一次性随机URL；库中只存token哈希，服务凭据由MASTER_KEY通过AES-GCM加密。

|格式|链接后缀|
|---|---|
|sing-box完整JSON，回环mixed:1080|无参数或?format=sing-box|
|Mihomo/Clash Meta JSON（合法YAML）|?format=clash|
|Base64 URI列表，2022按SIP002百分号编码，IPv6加方括号|?format=base64|

并非所有旧Clash内核支持2022。分享链接仅使用配置域名，不信任任意Host生成外链；公共8091处理器只有/s/{token}，没有管理和Agent路由，请求Host也必须匹配。默认每秒全局20次下载，固定内存计数；超限429。no-store，网关关闭该位置访问/错误日志。

只有启用、版本吻合、45秒内回报运行的服务会下发，全部不可用返回503。轮换、撤销、过期使旧URL无效，不能撤销已经导入的代理密码；泄露后停用并替换服务。域名变更后旧Host被拒绝，既有客户端要更新订阅域名。

## 网络场景

|场景|接入方式|前提|
|---|---|---|
|公网IPv4|A记录/IPv4入口|云安全组与宿主入站允许实际端口|
|仅公网IPv6|AAAA或IPv6地址、IPv6监听|客户端具备IPv6，保留ICMPv6/邻居发现|
|家庭宽带/CGNAT无公网IPv4|设备Agent、FRP客户端主动连接中继|至少一台可达公网/IPv6中继|
|IPv4-only客户端访问IPv6-only设备|双栈/IPv4中继映射到设备FRP客户端|中继提供IPv4入口，不能直接靠IPv6地址或域名互通|
|内网控制面|先通过独立FRP/VPN建立可达TLS网关|首次接入不能循环依赖尚未连上的控制面|

本版FRP提供受限TCP映射，未提供UDP、HTTP虚拟主机、XTCP、虚拟网卡。预留租户字段不是FRP租户隔离，不适合直接商用共享中继。


## 显式客户端格式与手机管理

新建或轮换订阅后，在客户端格式下拉框选择 sing-box、Mihomo / Clash Meta 或 Shadowrocket，再复制链接。分别使用 format=sing-box、format=mihomo、format=shadowrocket；旧 clash/base64 参数继续兼容。v0.6 新增协议支持范围和 Snell 导出边界见 [协议配置](PROTOCOLS-v06.md)。

总管理端可显式给 domain-sync.py 增加 --public-management，以允许任意来源访问 HTTPS 控制台，管理员令牌仍必需；底层 8080/5173/8091 继续只监听回环。该选项拒绝 HTTP，不影响订阅和 Agent 的独立接口。默认仍保持管理员 CIDR 限制。
