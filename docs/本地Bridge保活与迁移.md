# 本地 Bridge 保活与迁移

DigCrawl 的 MCP 仍通过 `digcrawl mcp --config <实际 profile>` 接入。socket 和运行状态由 DigCrawl 管理。运行状态保存在 `os.UserConfigDir()/digcrawl/run`，macOS 对应 `~/Library/Application Support/digcrawl/run`。安装、升级与清理缓存时保留该目录。profile 的规范化绝对路径继续作为实例身份，socket 路径超过平台支持长度时启动会明确拒绝。

## 更新与回滚

按单用户、单机器允许短暂停服设计。停止/重启、profile 整理、安装发布及后台故障恢复共用私有运行目录里的一个既有资源锁，锁覆盖操作准备到收尾的全过程。第二个维护命令在修改状态前返回“已有维护进行中”，待前一操作完成后重试。状态查询与正常业务请求不争用该维护锁。

使用本仓库 Shell/npm 安装器更新时，原生 guard 在安装资源锁和维护锁内完成：核验指向待替换程序的 LaunchAgent → 持有对应 profile 的维护停止标记 → 停止旧 watcher → 排空已接收请求 → 关闭 Bridge 和浏览器 → 按原工作目录整理标准 profile 配置 → 原安装器发布程序及资产 → 释放本次维护标记 → 重新加载原 LaunchAgent。两类安装器不再单独调用 browser migrate，避免以安装器目录重复解释相对路径。自定义 watcher profile 同样参加交接；其他程序路径的 watcher 保留。用户主动 `stop` 产生的 `.paused` 不会被安装器清除。

安装准备失败、父进程关闭控制管道或收到正常终止信号时，恢复出口也会尝试重新加载原定义。加载失败会令安装返回错误，不报告成功。安装完成 ACK 表示交接完成；服务 ready 仍需实际 manifest 探测，浏览器能力仍由 doctor 单独核验。安装器不会承诺把之前已发布的程序或配置自动回滚。

macOS 的 Bridge 和浏览器归属统一绑定内核进程创建时间，避免调用方时区、语言或进程标题改变影响停止判断。旧指纹通过原有命令/socket/进程组核验，并限制对应进程不得晚于归属记录创建；新版记录直接使用精确创建身份。

更新前关闭该 profile 的旧 MCP 代理，使用更新后的实际二进制执行以下命令。

```sh
digcrawl bridge restart --config /absolute/profile.yaml
digcrawl bridge status --config /absolute/profile.yaml
digcrawl doctor --config /absolute/profile.yaml
```

首次 MCP 或 watcher 启动会在持有新旧生命周期锁、确认旧实例已退出之后，先迁移恢复历史，再预约启动额度。已有稳定目录记录作为后续重连的权威历史；状态和 doctor 在首次迁移前也读取旧预算，查询保持只读。

`restart` 先持久暂停自动恢复，再停止新目录和旧缓存目录中经核验的进程，等待浏览器及锁释放，迁移旧恢复记录，最后恢复启动。新的 Bridge 持有稳定目录锁，同时持有旧路径兼容锁，浏览器资源锁也使用相同兼容策略。旧缓存目录已被清理时仍会检查引用旧 socket 的进程。旧 MCP 代理可能继续使用旧路径，应在版本切换时退出，再通过原配置重连。

停止/重启先取得整段维护锁，再发布暂停并读取配置。没有并行接管、后发覆盖、暂停代际或动态数据搬迁。restart 在停止和配置检查成功后解除暂停并冷启动；取消或在这些阶段失败时保留暂停。控制操作只写私有运行目录，配置所在目录可以只读。

正常停止通过私有 socket 请求 HTTP server Shutdown：不再接收新任务，等待已接收请求结束后才清理浏览器并释放生命周期锁。已接收请求的完成包含工具执行及 MCP 所需的附件交付，图片等附件与结果在同一次私有响应中传回 stdio。stdio 后续的格式转换和输出使用本次调用已有的数据，不再依赖运行中的 Bridge；任务没有被取消时，维护不会把已成功的图片读取变成附件获取错误。等待没有固定的强杀截止时间，任务仍受原有工具超时约束；可取消控制命令的等待，取消不会打断正在排空的任务。停止完成由生命周期锁及浏览器资源释放核实，停止请求 ACK 只代表开始排空。Unix socket 丢失时仅向身份及程序均可核验的新版 Bridge 主进程发送 TERM；Windows 无法建立连接时明确报错，不强杀任务树。

首次从不支持排空停止的旧版本升级时，先等待旧任务结束，使用旧版本停止 Bridge，再进行替换及重新连接。新版检测到旧服务不支持停止协议会拒绝在线替换，不猜测任务是否结束。手动覆盖了仍运行程序的磁盘文件属于不受支持的更新顺序，应按下述停止再替换流程操作。

回滚先用新版 `bridge stop` 停止新目录实例，再替换为已保存的旧二进制，最后重新连接旧 MCP。新版暂停标记保留在稳定目录，之后重新升级时使用新版 `bridge restart` 恢复。不同版本的调用方需按实际 profile 协调切换。

手动替换程序且启用了 watcher 时，先 `bridge watch --uninstall`，完成对应 Bridge 的停止和程序替换，再使用目标版本 `bridge watch --install`。新 watcher 每轮核对运行程序与安装路径的字节身份，程序已被替换就退出，由 launchd 重新执行；已经运行的历史 watcher 没有这项保护，应通过安装器或上述显式卸载/重装流程切换。

强制杀死整个安装进程树或断电可能留下 `.stop-*`。这是保守的未完成维护信号；确认原操作及相关进程均已结束后再按实际 profile 核验处理，不能批量清除停止标记。普通退出只移除该操作自己的标记。

## 查询状态

```sh
digcrawl bridge status --config /absolute/profile.yaml
```

命令输出 JSON。`ready` 表示实际 manifest 请求及构建身份校验通过。`reason`、`owners`、`remaining_fast_recoveries`、`cooldown_until` 和 `first_failure` 提供本地诊断。暂停或停止期间，即使进程尚在退出，命令也返回非零。查询只读取文件和进程状态，既不建立目录，也不调整权限或启动浏览器。

`first_failure` 保留本轮首次启动失败的时间、阶段和固定原因，文件大小受控，内容不包含原始异常、配置正文或凭据。显式重启成功后清除该记录。实际目录无法写入时，以命令的启动错误排查权限。

## 持续巡检

按需 MCP 使用保留原入口。需要常驻时，使用同一个 DigCrawl 二进制为实际 profile 安装用户级 LaunchAgent。

```sh
digcrawl bridge watch --config /absolute/profile.yaml --install
```

也可以直接在前台运行 `digcrawl bridge watch --config /absolute/profile.yaml`。macOS 用户级服务只在对应用户已登录时运行。watcher 刚启动且没有旧进程归属时可直接按原 MCP 启动路径冷启动；持续运行时每 30 秒通过真实 socket 请求 manifest，连续两次失败后尝试恢复。休眠空档不会计入失败次数或连续健康时间，唤醒后重新探测。

安装 watcher 时，既有 `XDG_DATA_HOME`、`DIGCRAWL_BROWSER_DIR` 浏览器位置覆盖会转换成绝对路径写入同一 LaunchAgent 的环境配置。更新时保留其原定义；改变这些部署路径后，用目标环境重新执行 `watch --install`。历史 plist 若只有 HOME，仍按该 HOME 下的默认浏览器目录运行。

相对配置路径继续以调用方工作目录为基准。`watch --install` 将当时的工作目录保存到原有 plist 的 `WorkingDirectory`，后台恢复和安装交接均保留此基准；内部 `digcrawl/run` 只保存运行状态，不再充当应用工作目录。安装器和手动 profile 整理使用已有 watcher 记录的工作目录；没有记录时沿用调用目录。包含相对 `UserDataDir` 的 profile，手动 stop/restart 仍应在原项目目录执行。记录的工作目录需保持存在，应用项目目录确需变更时，先在原目录等待任务结束并停止 Bridge，再从目标目录重新安装 watcher 并重启；不进行运行中目录迁移。绝对资源路径不受工作目录影响。

历史 plist 若已错误地把内部运行目录作为工作目录，升级不能可靠推断原项目位置。应使用更新后的 CLI，从实际项目目录重新执行一次 `bridge watch --install --config /absolute/profile.yaml`，保留原浏览器目录，不搬迁数据。自带历史定义的绝对工作目录在普通升级中继续保留；缺少或使用相对工作目录的异常定义会明确拒绝接管，要求重新安装。

普通故障保留最多 3 次快速自动恢复。额度耗尽后进入 5 分钟冷却，每个冷却周期允许一次受控尝试，10 分钟连续成功的 RPC 观察恢复快速重试额度。健康观察间隔超过 1 分钟时重新计时。老记录缺少可信的最后尝试时间时，使用显式重启恢复。

恢复沿用原生命周期锁及精确进程身份核验。Bridge 的 `.owner` 记录同时保留各个专用浏览器的 PID、私有进程组和启动身份列表，浏览器对外可用前完成原子写入。Bridge 主进程退出后，替代实例在持锁期间重新核验浏览器身份，再回收已确认的残留进程组。正常退出的浏览器资源已释放后清理记录，清理失败时保留归属信息。

旧版两字段或三字段 `.owner` 记录继续支持读取。历史记录缺少浏览器身份，或浏览器组长已退出且剩余成员无法核验时，恢复会保持阻断并要求本机核验。Rod 默认的 leakless 父进程退出清理继续生效。

构建不匹配、无法验证的进程归属和权限拒绝会报告原因。主动停止使用持久暂停标记阻止巡检和 MCP 按需启动。恢复服务使用 `bridge restart`。恢复建立服务连通性，已经派发且结果未知的工具调用保留原执行语义。

```sh
digcrawl bridge stop --config /absolute/profile.yaml
digcrawl bridge restart --config /absolute/profile.yaml
digcrawl bridge watch --config /absolute/profile.yaml --uninstall
```

移除巡检服务只移除对应 profile 的 LaunchAgent。Bridge 的停止由显式 `bridge stop` 完成。watch 日志保存在稳定运行目录内，以 profile 哈希区分。开发测试应使用独立 HOME、profile 和浏览器资源，保留在线实例的运行目录。
