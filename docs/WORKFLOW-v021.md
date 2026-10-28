# 0.21 节点名称与依赖自动安装

订阅、策略预览、端点勾选及 URI / JSON / YAML 共用一个命名函数：IPv4 与域名使用后台原名，IPv6 使用原名-v6，包括仅 IPv6 的入口。IPv4 映射 IPv6 地址按 IPv4 处理。内部仍用 endpoint:ID:ipv4 / ipv6 / dns 及 node:ID 绑定，改名不会改变用户权限或把单选地址扩大为双选。新导入原名匹配 IPv4 / 域名，原名-v6 匹配 IPv6；旧原名-ID8-ipv4/ipv6 别名继续精确识别。旧绑定 ID 保持已有语义；新原始配置的直接端点名绑定到对应地址族。删除会清理端点引用。

不同节点的最终名称重复（包括 A 的 IPv6 与名为 A-v6 的另一 IPv4 节点），或与策略组 / 客户端保留名称冲突时，分享检查会提示改名；不自动拼随机 ID，不输出重名配置。名称不能有逗号、控制字符或首尾空白。

Linux 引导命令先补齐 curl，再由发行安装器检测 Python >=3.9 与 Docker；缺少时使用发行版软件源（RPM 系使用 Docker 官方仓库），启动 Docker 后验证 Linux 容器引擎。已有引擎不替换、不清理。Windows 通过 WinGet 自动安装缺失的 Python 与 Docker Desktop，需要时用微软官方模块安装 WinGet；WSL 新启用若需重启返回 3010，用户重启后重跑同一命令。第一次许可与 BIOS 虚拟化仍须由用户处理；不强制重启或切换已有 Windows 容器。

环境准备完成后才解压经 SHA-256 校验的架构包和创建面板状态；数据库只绑定回环，不使用远程 Docker 上下文。成功后清理本次下载安装缓存，失败保留用于诊断。Windows 的系统环境修改使用官方安装器；不绕过组织脚本执行策略。纯环境安装可用 Linux --prerequisites-only、Windows -PrerequisitesOnly。

卸载、管理员登录令牌在 README 中分为独立标题及独立代码框，避免误执行。具体手动与离线方式继续见 INSTALL.md。

参考：[Docker 安装](https://docs.docker.com/engine/install/)、[Docker Desktop Windows](https://docs.docker.com/desktop/setup/install/windows-install/)、[微软 WinGet](https://learn.microsoft.com/windows/package-manager/winget/)。
