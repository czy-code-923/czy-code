# v0.7 节点配置模型

主机（Agent）→ 部署实例（选择内核）→ 逻辑节点的协议入站/出站 → 链式路由或 HTTP 反代 → 客户端订阅。内核和协议分开选择。逻辑节点可跨主机复用，每个部署目前启动一个有资源限制的引擎进程；不是所有逻辑节点合并成一个进程。

## 面板入口

逻辑节点：选择主机、sing-box/Xray/FRP，再选兼容协议。详情/编辑显示监听、连接地址、传输、安全层、证书引用、出站下一跳和脱敏原生配置。手动密码、UUID和私钥只写不回显，留空保留当前凭据；可随机轮换。保存使用期望版本，冲突返回409。主机和协议切换使用替代部署。FRP 的共享隧道拓扑仍采用替代实例，不能在已有客户端绑定时原地重写。

已实现：sing-box 的 Shadowsocks2022、VLESS、VMess AEAD、Trojan、HY2、TUICv5、AnyTLS、Snell；Xray 的 VLESS、VMess、Trojan。VLESS/VMess/Trojan 支持 TCP / WebSocket。VMess 可选外层 TLS，协议自身并不依赖证书；TLS 模式仍验证公共信任链、域名及私钥。VMess 固定 AES-128-GCM，避免不同内核在 TLS 下 auto 加密协商不一致。

兼容目录来自官方文档，按内核分别列出入站/出站；“官方文档能力”不等于当前固定版本/构建一定具备，更不等于平台已提供模板。REALITY、Vision、gRPC、XHTTP、WireGuard、TUN 和其他未集成类型明确标记，不能使用原始任意 JSON 绕过。

## 链式出站

当前部署选择下一跳，下一跳仍执行自己的服务端出站。最多8跳，拒绝环路。sing-box 可使用上述已实现节点作出口；Xray 仅开放 SS2022/VLESS/VMess/Trojan 出站。Agent 每次同步解析最新下一跳凭据，配置修改使依赖入口版本失效并重载。订阅仅导出勾选的入口端点，不把服务端完整链路和出口凭据下发给客户端；只有入口及依赖链全部新鲜回报运行才可导出。使用系统公网 DNS 地址时还需自行确保节点地址和业务端口可达。

## 免费证书

证书按域名独立管理，免费一键申请将请求放入持久化队列。HTTP-01 需要域名 A/AAAA 指向总管理网关且 TCP80 可达；Cloudflare DNS-01 需该区域的最小权限 DNS编辑/API令牌，适合无公网IPv4/仅IPv6/NAT设备。DNS凭据和证书私钥加密存储。通配符暂不支持。TLS节点选择证书引用后，续期会更新引用部署并推动配置版本；不再要求新建代理服务。

安装 root 受限网关执行器 `scripts/managed-gateway.py`，每分钟轮询，证书到期前30天检查自动续期，失败保留旧证书并在面板显示失败。执行器只调用 root可信 certbot/nginx 固定参数，不接受 shell。Cloudflare需要官方系统包 `python3-certbot-dns-cloudflare`。申请按钮的确认说明 Let's Encrypt 订户协议，由操作员点击确认。

## HTTPS反代

总管理网关提供独立域名反向代理，目标是本机 HTTP 应用或已建立的 FRP 映射端口，可用于内网应用发布。支持 WebSocket，强制 HTTPS 和匹配的受管证书；拒绝覆盖管理域名及访问已知管理/数据库端口。保存、修改、启用/停用均在页面操作，有期望版本和网关回报。Nginx配置检查失败恢复前版本。当前并非任意远端主机 Nginx 管理，也不等于 Xray 的 reverse 隧道协议。

## 安装网关执行器

```sh
apt-get update
apt-get install certbot python3-certbot-dns-cloudflare
# scripts/domain-sync.py 与 managed-gateway.py 放在 root 拥有的同一目录
# root私有环境 /etc/harbor/control.env 只读ADMIN_TOKEN，权限0600
python3 /opt/harbor/scripts/managed-gateway.py --env /etc/harbor/control.env --nginx /www/server/nginx/sbin/nginx --vhost-dir /www/server/panel/vhost/nginx
```

应作为 root systemd oneshot/timer 每分钟运行；不授权普通 Agent 修改系统网关。节点协议新功能要求 Agent0.7.0，旧 Agent 不接收无法识别的新规格。后台20秒低频轮询，隐藏页面暂停，输入编辑时暂停，未启用实例不启动引擎。

官方参考：
- https://sing-box.sagernet.org/configuration/inbound/
- https://sing-box.sagernet.org/configuration/outbound/
- https://xtls.github.io/en/config/inbounds/
- https://xtls.github.io/en/config/outbounds/
- https://letsencrypt.org/docs/challenge-types/
