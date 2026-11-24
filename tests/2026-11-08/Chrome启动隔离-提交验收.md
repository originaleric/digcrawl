# Chrome 启动隔离与安装升级：提交验收

本次提交将后台浏览器固定为受校验的独立 headless shell，交互登录使用独立应用身份的 Chrome for Testing。普通池、多实例、登录、独立检查、Bridge 迁移和可选 CDP 服务遵循同一运行时与所有权边界。

安装保持 shell/npm/Homebrew 现有分工，复用既有 OS 文件锁，覆盖实际共享资源；异常失去 owner 后停止后续发布，正常释放确认后才报告完成。配置迁移以完整有效配置为真值，仅允许 ChromePath/Headless 的差异，保留原始备份及其他配置消费者。

系统复查覆盖隔离、旧入口、共享资源竞争、失败清理、进程身份、释放屏障、YAML 引用与继承、重复执行及旧协议拒绝。当前范围未发现尚未修复的 P0/P1/P2 findings。

已有本地证据：全量 Go、相关 race、npm 17项、完整 shell 安装、Homebrew hook、24种配置组合、原生单/多资源锁的 release/EOF/SIGTERM 场景，以及真实头条登录与会话复用。

本次与公共 main 对齐后，在当前目录针对实际暂存源码重新验收，以下检查全部通过：

- `go test -p 1 ./... -count=1`。
- `npm test`：17项通过。
- `sh scripts/test-install.sh`：完整安装、重装、共享锁拒绝、迁移和适配器验收通过。
- `go vet ./...`、CLI 构建与暂存差异格式检查。
- `go test -race -p 1 ./transport/bridge ./cmd/digcrawl -count=1`。

未提交的 source.discover 文件在验收结束后原样恢复，不纳入提交。按用户授权执行的 Linux/macOS/Windows 远端 CI，以该提交对应的 GitHub Actions 结果为准。普通 CI 保持手动触发，Release 标签流程不变，本次不发布版本。

首轮远端 Linux/macOS 全部通过；Windows 发现备份测试误把 POSIX `0600` 当作跨平台权限表示。测试已调整为所有平台校验普通文件类型、备份内容和幂等，Unix 额外校验 `0600`；Windows ACL 不冒充为已通过模式位检查。迁移实现未改变，配置全包本地回归通过，随后对新提交重新执行三平台 CI。

原生 CI 不代替 Google 真实账号认证或日常 Chrome 全退出冷启动验收。跨文件断电原子性、不参加新协议的旧安装器及外部强制覆盖仍不在保证范围内。原始运行日志、Cookie、私有配置和构建产物仅保留本机。
