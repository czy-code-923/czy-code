# 本地运维

启动：`docker compose up --build -d`；查看：`docker compose ps`、`docker compose logs api`。

停止保留数据：`docker compose down`。重启后迁移幂等，资源、配置、任务和审计都从 PostgreSQL 恢复。Agent 无本地状态，未回报任务在过期后由同主机 Agent 重领。

Linux / macOS 备份：

```sh
docker compose exec -T db pg_dump -U harbor -d harbor -Fc > harbor.dump
# 停止写入后，把备份恢复到单独的空库验证；不要盲目覆盖现有库。
docker compose exec db createdb -U harbor harbor_restore
docker compose exec -T db pg_restore -U harbor -d harbor_restore < harbor.dump
```

PowerShell 的二进制重定向在旧版本可能损坏备份，使用容器内生成再复制：

```powershell
docker compose exec db pg_dump -U harbor -d harbor -Fc -f /tmp/harbor.dump
docker compose cp db:/tmp/harbor.dump ./harbor.dump
docker compose cp ./harbor.dump db:/tmp/restore.dump
docker compose exec db createdb -U harbor harbor_restore
docker compose exec db pg_restore -U harbor -d harbor_restore /tmp/restore.dump
```

备份包含库存、配置和令牌摘要；`.env` 中令牌需另外安全保存。丢失 Agent 原始令牌后通过管理 API 轮换，不从数据库恢复原文。

健康检查 `GET /healthz` 只检查数据库可达；其余列表 API 需鉴权。Agent `last_seen` 是原始心跳时间，本版不自动推算在线/离线状态。任务一直 queued 时检查对应 HOST_ID、令牌和 Agent；running 时等待 30 秒租约到期再重试领取。

演示脚本只为一台主机提供 Agent 环境变量，所以另外两台的排队任务不会被它领取。

v0.2 控制面每 5 秒独立回收过期的 running 任务；未耗尽三次则排回 queued，耗尽则 failed。queued 任务不会因主机离线而自动取消。完成回报支持同完成租约/结果/摘要重试。

开启原生校验暂存时还需备份 STATE_DIR 与 ENGINE_SECRET，它们不在数据库备份内。升级 003 会检查跨资源端口冲突；冲突时迁移事务整体回滚，先在旧版本修正库存再升级，不能删除旧数据库或强行跳过迁移。具体暂存锁恢复和本地指针对账见 STAGING.md。
