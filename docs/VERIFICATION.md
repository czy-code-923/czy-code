# v0.2 验证记录

验证日期：2026-10-20，Asia/Shanghai。审查结论与修复清单见 REVIEW.md。

## 已执行并通过

|检查|证据与范围|
|---|---|
|Go 后端|Go 1.27.3 / pgx 5.11.0；`go test -count=1 -v ./...` 通过，已提供数据库及官方引擎路径，没有集成测试跳过；`go vet ./...` 通过|
|真实数据库|本机 PostgreSQL 17.6，回环 55432；每组测试隔离 schema；并发领取、完成/入队竞争、租约回收/续期、幂等、权限与资源约束通过|
|升级|旧 001/002 数据制造跨表端口冲突后，003 迁移失败并整体回滚；修正后升级和预留回填通过；已有演示库实际从 v0.1 升级成功|
|清单验证|非回环监听/目标、重复端口、监听与隧道冲突、错误字段、错误摘要被拒绝|
|原生程序|官方 sing-box v1.14.3、FRP v0.71.1 的 Windows amd64 发布归档下载后校验 GitHub 发布元数据 SHA-256；只运行 check / verify|
|真实原生配置测试|sing-box block / direct 均通过 check；FRP server/client 两份 JSON 均通过 verify；历史暂存版本重新发布通过|
|失败保护|坏引擎摘要、校验失败、过期租约、路径穿越、第二个写入进程、已损坏历史文件被拒绝；旧暂存指针保留|
|端到端|独立控制面 + 编译 Agent + 官方引擎：2 个 sing-box 配置暂存 + 1 次历史回退 + 1 个 FRP 配对暂存，4 个任务成功；本地 current.json 与控制面版本一致|
|前端|`npm test` 3 个实际 Vue 组件测试通过：退出后迟到轮询不恢复会话、迟到一次性凭据不展示、后台轮询不清除操作错误；类型检查及生产构建通过|
|浏览器|验证登录/退出、配置列表、原生预览凭据隐藏、历史恢复入口、任务结果；最终截图 preview-v02.png|
|依赖|`npm audit --registry=https://registry.npmjs.org` 本次报告 0 vulnerabilities，含开发依赖；不代表整体平台不存在其他风险|
|交付配置|OpenAPI 3.0.3 经规范校验；Compose 主文件和 staging 覆盖 YAML 静态解析；只发布本机 API/前端，数据库无宿主端口|
|跨平台编译|控制面与 Agent 的 Linux amd64 / CGO_DISABLED 编译通过，未在 Linux 执行|

旧演示的另外两台库存主机仍没有 Agent，所以两个 simulation 任务保持 queued，符合隔离预期。原生测试仅新增 inventory.invalid 库存，未接入真实机器。检查测试用代理/FRP 端口没有监听，也没有 sing-box/frps/frpc 常驻进程。

## 本次官方归档来源与摘要

- [sing-box v1.14.3 Windows amd64](https://github.com/SagerNet/sing-box/releases/tag/v1.14.3)，归档 SHA-256：`d8b86ef58936dba9230ec6a57ffa4243933f3c14ef4809828b6c553440ed1518`。
- [FRP v0.71.1 Windows amd64](https://github.com/fatedier/frp/releases/tag/v0.71.1)，归档 SHA-256：`b90074b2b6a1d792a7a40f085ac6d276bcab2d1bc60201f8a86ca53ce65ce821`。

实际 Agent 校验的是解压后各个可执行文件的 SHA-256；不要把上述归档摘要填到 *_SHA256 环境变量。没有将第三方引擎可执行文件放进源码包。

## 未执行 / 尚未交付的运行范围

- 当前机器无 Docker：未实际构建镜像或启动 Compose，静态解析不等于容器验证。staging 覆盖文件也同样未在容器运行。
- 未在 Linux 上运行 CI / race 检测、systemd、真实端口预检、服务健康探测、运行服务回滚。
- 未连接真实 VPS、未启动代理/FRP 服务、未开放公网端口；原生 check/verify 的成功不证明真实转发或跨机隧道连通。
- 未实现生产 HTTPS/mTLS 登记、账号/RBAC、用户自助、订阅/计费、跨机 FRP 拓扑或真实中转编译。
- 暂存的本地文件与数据库不具备分布式原子性；断电/长时间网络中断仍需对账，详见 STAGING.md。审计不防御数据库管理员。

工具、官方二进制、演示数据及校验暂存状态只保留在工作区 work/，不包含在源码压缩包中。正式启动按 README 生成新的 .env 和自己的数据库卷。
