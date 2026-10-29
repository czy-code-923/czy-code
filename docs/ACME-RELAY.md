# 远端 HTTP-01 验证转发

控制面当前在总管理机执行 Certbot。域名解析到其他 VPS 时，可以在该 VPS 的空闲 TCP 80 上单独运行 `scripts/acme-relay.py`，将匹配域名且路径为 `/.well-known/acme-challenge/<token>` 的 GET 请求重定向到管理网关的 HTTP 验证目录。其他请求返回 404。请先确认 DNS、80 端口及所有 A/AAAA 记录均可达；已有 Web 服务时应在原 Web 服务中配置对应路由，不要抢占端口。

示例（以非特权测试端口运行，正式 80 端口请使用受限的 systemd 服务及 CAP_NET_BIND_SERVICE）：

```bash
python3 scripts/acme-relay.py --domain node.example.com --target http://panel.example.com --port 8080
```

这是单独部署和维护的工具，当前面板不会自动安装或卸载它；域名或管理网关变更时需同步修改，服务停止会影响后续 HTTP-01 续期。控制端验证目录由管理网关提供，使用前需通过公网端到端测试。仅验证请求使用 HTTP，不应传输管理令牌或代理凭据。DNS-01 可作为另一种方式。

