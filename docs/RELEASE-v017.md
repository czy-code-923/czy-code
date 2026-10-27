双栈节点现在可在策略组内分别勾选 IPv4 / IPv6 端点。空模板不再预置虚构协议节点；删除节点会清理策略引用。PROXY 只在被引用时生成并放在最后，定制模板保留。

改用官方 sing-box v1.14.4、Xray v26.10.21、FRP v0.71.1。主机新增官方内核版本页面，可更新最新稳定版或切换指定版本；校验摘要、当前配置和启动状态，失败回退。旧主机需先升级 Agent，再手动切换内核。

FRP 补充受控 UDP 映射和旧 Agent 保护。外部面板 API 增加用户单查、游标分页、external_id、乐观版本与创建幂等，保留额度、重置周期、用户组和订阅权限控制。

提供完整源码、Linux / Windows amd64 / arm64 包、Agent 包、校验安装脚本、对应官方 sing-box 源码与 SHA256SUMS。发布前运行后端/数据库与前端测试，正式标记前执行全新 Linux 安装、Agent 部署、官方内核更新和退役清理测试。详见 [0.17 工作流与边界](https://github.com/czy-code-923/czy-code/blob/main/docs/WORKFLOW-v017.md)。GitHub 发布不代表已有 VPS 自动升级。
