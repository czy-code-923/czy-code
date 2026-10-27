分享前用真实导出流程检查节点、授权、客户端兼容性、策略和流量额度，明确显示不可分享的原因。默认 Mihomo，支持手动切换；自动识别客户端优先于输出预设，自动和显式格式都过滤不兼容节点。

策略组默认全部授权节点，可切换为按稳定 ID 手动勾选。导入按协议、地址和端口唯一匹配；歧义要求手动映射，不会扩大为整个协议。内置模板根组包含 ALL，已有策略保留。

发布流程重新构建 Linux / Windows 的 amd64 / arm64 分发包，提供源码、Agent 包、安装脚本与 SHA-256 校验和。Go / PostgreSQL 和前端测试在发布前执行；全新 Linux 安装、Agent 接入、真实部署与退役清理测试通过后才标记为最新正式版本。Windows 分发包经过编译，Windows 本机服务安装仍应按文档核对 Docker Desktop 等前提。

本次生产订阅已在 Clash Verge 成功导入并通过 Mihomo 原生检查。详细行为与边界见 [使用说明](https://github.com/czy-code-923/czy-code/blob/main/docs/WORKFLOW-v016.md)。现有安装更新前请备份数据库及配置；发布到 GitHub 不代表现有 VPS 已自动升级。

新安装命令与手动构建方式见 [README](https://github.com/czy-code-923/czy-code#readme)。运行安装脚本前可使用附件中的 SHA256SUMS.txt 校验分发文件。
