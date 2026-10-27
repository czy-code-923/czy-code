# 0.10 验收 · 2026-10-26

- Windows 本地 Go 全包测试及 `go vet` 通过；PostgreSQL 17 独立 schema 验证 CRUD、主机隔离、停止版本确认、用户配额与幂等、重置、访问记录和试用清理。
- 固定版本原生内核运行协议转发、IPv4 / IPv6、链式代理、FRP、用户身份测试；sing-box / Xray 的真实转发同时验证用量计数和来源 / 目标元数据。修复版 sing-box 1.14.4-harbor.1 再跑通过。
- 管理 VPS 上先创建独立临时测试库，再运行 Linux API 集成测试和低权限真实内核测试，全部通过；测试库已删除。没有用生产用户流量伪造样本。
- 前端 18 项测试通过，生产构建通过。完整 npm 依赖审计 0 项，控制面 govulncheck 未发现可达已知漏洞。内核扫描的范围和剩余告警见 SECURITY-v010.md。
- 安装器 4 项隔离测试通过，含原安装保留、失败回滚、私有权限及 GPL 源码随包安装；域名同步器 3 项、受限网关 4 项测试通过。
- 真实管理端升级 Agent 0.10.0 / 修复版 sing-box，保持两条已启用入站和两条停用的普通配置。清理 12 个停用 Acceptance 验收配置与 4 条试用订阅。数据库及配置有回滚备份。
- 现场修复公开静态目录权限与 os.Root 目录替换后的句柄问题；发布后检查真实 HTTP 200。在线状态 / 计量状态改用数据库时钟，修复客户端与 VPS 时钟偏差造成的误报。
- 390×844 手机视口和桌面验收；修复表格撑宽整个页面的问题，手机 document.scrollWidth=375、innerWidth=390（浏览器滚动条占宽），无整页横向溢出。明暗主题检查通过，恢复默认视口与深色主题。

本轮没有实测其他真实 VPS 接入、公网 IPv6 / CGNAT 全拓扑、Windows 系统安装、防火墙接管、生产恢复演练或大规模压测。ARM64 为交叉编译，不能冒充 ARM64 真机运行。Windows 本机缺少 C 编译器，本轮未跑本机 race 检测；Linux CI 保留 `go test -race`，尚未收到本版 CI 结果。

升级现场服务组内存快照：control 47,841,280 B，agent（含子内核）133,365,760 B，合计约 173 MiB；不包含 PostgreSQL / nginx，不是压力测试峰值或所有部署的内存保证。

产品设计参考：

- https://docs.rw/learn-en/users/
- https://docs.rw/learn-en/nodes/
- https://github.com/shenaba/2s-ui
- https://sing-box.sagernet.org/configuration/service/api/
- https://github.com/XTLS/Xray-core/blob/main/app/stats/command/command.proto

内核安装器另有 4 项测试通过：预编译修复版不依赖 Go、摘要篡改拒绝、架构不匹配拒绝、已有目录不覆盖。四平台分发包自带修复内核、对应源码和校验回执。
