# v0.4 验证记录（2026-10-22）

## 当前本地验证

- Windows，Go1.27.3、PostgreSQL17.6；全部Go包test与vet。数据库测试设置TEST_DATABASE_URL，创建独立schema并清理。
- 官方sing-box1.14.3 / FRP0.71.1资产经过SHA-256校验；NativeForwarding子测试实际通过127.0.0.1和::1运行FRP服务端/客户端、sing-box两跳及测试客户端，成功读取本机HTTP目标，停止后监听端口释放。未连接外部代理目标。
- 新接口测试覆盖加密存储、域名校验、跨机权限、期望/回报版本匹配、离线新鲜度、服务停用、端口冲突、分享格式、URL轮换/撤销、独立分享处理器隔离管理路由。
- 既有库存、队列、租约、暂存、回滚、审计、防火墙fake-backend测试继续执行。
- Vue/TypeScript构建、前端16项测试；主题、会话、轮询、启停确认、域名保存与一次性分享链接。
- 域名同步器3项模拟测试：注入拒绝、成功/无变化不重载、nginx验证失败回退。使用fake nginx/HTTP，不改变本机nginx。
- Python语法、PowerShell AST、bash语法；Linux/Windows amd64/arm64交叉构建另附本次构建记录。

## 明确未验证

Docker Compose实际启动；Linux内核nft与systemd应用；宝塔nginx加载；ACME真实申请；VPS公网IPv4/IPv6和CGNAT链路；Linux父进程崩溃清理（已实现并由CI测试补充）；Windows计划任务实际安装。v0.4新UI未完成本轮浏览器视觉检查。

本轮桌面浏览器控制被工具停止；Windows SSH agent未运行，现有密钥认证失败，目标45.129.8.122未执行部署。不能把源码发布当成服务器已上线。GitHub CI结果须以对应提交的实际状态为准。

## 最终构建与独立进程验证

- Linux amd64/arm64、Windows amd64/arm64：control、agent、firewall、gateway四个程序交叉构建成功，ZIP完整性校验通过。
- 独立编译的Windows control + Agent端到端通过：隔离新库迁移、原生Web、真实IPv6服务、新鲜进程回报、sing-box官方客户端check、SIP002格式、链接轮换、停用。进程退出，隔离schema清理；不是只在同进程mock API。
- 最终前端JS约122.1KB / gzip45.2KB，CSS23.0KB / gzip5.7KB；官方npm registry审计0漏洞。
- OpenAPI3.0.3验证、Compose/工作流YAML解析通过；接口/分享后端映射仅127.0.0.1，数据库不映射端口。

此记录不替代目标机的Linux/ACME/公网验证，不宣称所有路线图功能已实现。
