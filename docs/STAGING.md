# v0.2 原生配置校验暂存与回退

这是下一阶段的本地可验证增量：类型化清单 → 原生配置 → 官方引擎固定校验入口 → 私有版本目录 → 更新本地暂存指针 → 控制面记录结果。

**staged 不等于 running。没有 systemd 启动/重载、开放端口、修改防火墙或连接真实 VPS。** 没有 mTLS 登记、安装器、原生服务的健康检查或自动运行回滚，完整远程运行仍按 ROADMAP 推进。

## 引擎行为

|引擎|生成的配置|唯一允许的校验调用|
|---|---|---|
|sing-box|带认证的回环 mixed 入口；direct 或 route reject 策略|`sing-box check -c <generated-file>`|
|FRP|同机回环实验用 frps / frpc JSON 配对；token 认证、TLS、显式远端端口白名单|`frps verify -c <generated-file>` 和 `frpc verify -c <generated-file>`|

FRP 两份配置描述**同机本地实验配对**，不声称当前实例模型已经描述远程内网设备。跨机服务端/客户端落点、证书信任、设备授权仍是下一阶段工作。清单中的 outbound 字段只影响 sing-box，FRP 的隧道内容由 tunnels 决定。

密码/token 由 Agent 的本地 `ENGINE_SECRET` 和实例 ID 经 HMAC 派生，控制面不持有原文；预览始终隐藏凭据。保留该密钥，轮换会改变引擎认证材料。模板不含 includes、外部 URL、插件、文件输出或系统代理设置。校验进程只继承最小环境，不传入管理/Agent 令牌，校验原始输出不会返回 API 或进入日志。

原生模板依据：[sing-box mixed 认证](https://sing-box.sagernet.org/configuration/inbound/mixed/)、[route reject](https://sing-box.sagernet.org/configuration/route/rule_action/)、[FRP 配置验证](https://gofrp.org/en/docs/features/common/configure/)、[FRP 服务端端口白名单](https://gofrp.org/en/docs/reference/server-configures/)。

## 显式开启

默认 `go run ./cmd/agent` 仍只模拟。运行校验暂存 Agent，需要自行从官方发布页准备可信平台匹配的引擎程序，核验发布归档摘要，再设置每个**解压后的可执行文件** SHA-256：

```sh
export AGENT_MODE=staging
export STATE_DIR=/absolute/private/harbor-state
export ENGINE_SECRET='YOUR_STABLE_RANDOM_SECRET_AT_LEAST_32_BYTES'
export SING_BOX_BINARY=/absolute/trusted/sing-box
export SING_BOX_SHA256='SHA256_OF_EXECUTABLE'
export FRPS_BINARY=/absolute/trusted/frps
export FRPS_SHA256='SHA256_OF_EXECUTABLE'
export FRPC_BINARY=/absolute/trusted/frpc
export FRPC_SHA256='SHA256_OF_EXECUTABLE'
# CONTROL_URL、HOST_ID、AGENT_TOKEN 同 README
go run ./cmd/agent
```

PowerShell 使用 `$env:NAME='value'` 设置同名变量。必须使用绝对引擎路径，Agent 不搜索 PATH、不接收 API 提供的执行路径/参数，也不下载软件。只部署其中一种引擎时可以只配置对应程序；遇到缺少的程序会报告任务失败。

可信引擎目录和 STATE_DIR 必须由 Agent 账号私有管理，不能让托管服务或其他低信任用户写入。摘要检查不防御本地管理员替换文件的能力。

可选 Compose 暂存：准备 Linux amd64 的 `engines/sing-box`、`engines/frps`、`engines/frpc`，在 `.env` 补全对应文件的 SHA256 和 ENGINE_SECRET；登记主机得到 HOST_ID/AGENT_TOKEN 后：

```sh
docker compose -f compose.yaml -f compose.staging.yaml --profile demo up --build -d
```

只读引擎目录挂载到 `/engines`，可写私有卷只挂在 `/state`，没有服务管理套接字/宿主系统目录挂载。容器版本尚未在本机实际启动，见 VERIFICATION。

## 管理台与 API

- 配置版本 → **原生预览**：读取隐藏凭据的原生 JSON，不写文件。
- **校验暂存**：`POST /api/v1/instances/{id}/deploy`，body `{"config_id":"...","mode":"staging"}`，需要 Idempotency-Key。
- staging Agent 心跳报告 mode 后才可领取 staging 任务；仍可以完成 simulation 任务。模拟 Agent 不会领取 staging 任务。
- 暂存过至少两个版本后，在已成功暂存的非当前版本旁选择 **回退此版本**，通过 `/rollback` 创建新任务。没有成功历史、相同当前版本、跨模式成功记录都不能作为回退目标。
- 任务列表包含 mode、operation 和 artifact_checksum，列表不再返回活动 lease_token。
- `/agent/tasks/{id}/renew` 只为当前有效且属于本主机的租约续 30 秒。暂存前续租失败则保留旧指针。
- 完成回报相同租约、结果和摘要会幂等返回成功；不同结果、旧租约或别的主机被拒绝。

## 文件与故障语义

```text
STATE_DIR/
  .agent.lock
  instances/<instance-id>/
    current.json                  # 只有已通过校验的版本可成为当前暂存
    revisions/<task-id>-<lease>/
      sing-box.json               # 或 frps.json / frpc.json
```

生成文件只在 `os.Root` 限定目录内操作；任务/实例/配置/租约 ID 必须是固定格式，拒绝路径穿越。文件使用 0600、目录 0700（Windows 仍需由操作系统 ACL 保护）。固定校验命令每个最多 10 秒，无 shell。校验失败、摘要错误、租约过期不会发布新 current.json。原生配置按版本保留，回退重发曾成功版本，更新新的暂存指针，不删除历史。

临时指针先 Sync/Close 后重命名替换；这是进程级失败保护，**没有宣称跨操作系统断电持久性或数据库与本地磁盘的分布式原子性**。发布本地文件后如完成回报丢失，Agent 重试回报；崩溃/长时间断网后靠租约重试再次暂存。遇到磁盘与控制面版本不一致时需对照指针/摘要检查，生产阶段还需独立对账器。

同一 STATE_DIR 只允许一个 Agent；第二个进程会拒绝启动。正常退出清除 `.agent.lock`，强制终止/崩溃可能留下锁，必须确认旧进程已停止再由本地管理员清理它。不会自动删除锁或绕开它。Windows 测试脚本的强制结束也可能留下已停止测试进程的锁。

## 重复验证

官方引擎测试：设置 `TEST_SING_BOX_BINARY`、`TEST_FRPS_BINARY`、`TEST_FRPC_BINARY` 绝对路径后运行 `go test -count=1 -v ./...`。未设置时该原生集成测试明确跳过。

端到端检查：先启动控制面，编译 `go build -o agent ./cmd/agent`，设置 ADMIN_TOKEN、AGENT_BINARY 和上述六个引擎路径/摘要变量，再运行：

```sh
python scripts/smoke_staging.py
```

脚本只接受回环控制端，会新增带 `Native smoke` 标签的库存，并验证 sing-box 两版暂存、历史回退、FRP 配对暂存；不会启动服务。私有状态保留在 `state/smoke-...`，可用 SMOKE_STATE_PARENT 另选目录；它是一次性测试状态目录，不用于继续启动生产 Agent。
