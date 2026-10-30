# Cronplus

高级 Cron 任务管理器 — Go + Cockpit WebUI 构建。

[English](README.md)

## 架构

```
cronplusd (守护进程)
├── Scheduler (调度器)      解析 cron 表达式，触发任务（锚点步进间隔）
└── Executor Pool (执行器)  每任务独立进程，超时/重试/日志

cronplus (CLI)              命令行管理工具（实时流式输出，信号转发）
```

## 特性

- **6 字段 cron** 支持秒级精度（`秒 分 时 日 月 周`）
- **锚点步进间隔** — 编辑首次预测运行时间作为锚点，后续时间从锚点+间隔计算
- **每任务进程隔离** — 独立进程组，互不干扰
- **超时与重试** — SIGTERM → SIGKILL 递进终止，可配置重试次数与间隔
- **Run ID 追踪** — 每次执行分配唯一 ID（`YYMMDD-HHMMSS-xxxx`），全链路可追溯
- **手动运行** — 实时流式输出，Ctrl+C 终止，信号自动转发到子进程组
- **多用户支持** — 可以任意系统用户身份运行任务
- **Cockpit WebUI** — 任务管理、日志查看、服务日志、JSON 编辑器、9 种语言、深色/浅色主题、SVG 矢量图标
- **安全加固** — 环境变量白名单、原子写入配置、文件权限强制（0600）

## 安装

```bash
sudo dpkg -i cronplus_<version>_<arch>.deb
sudo dpkg -i cockpit-cronplus_<version>_<arch>.deb
```

安装后守护进程自动启动。

## CLI 使用

```bash
cronplus status              # 查看状态（含正在运行的任务）
cronplus list                # 列出任务
cronplus ps [--json]         # 查看正在运行的任务（来自守护进程控制 socket）
cronplus run <id>            # 手动执行（实时输出，Ctrl+C 可终止）
cronplus stop <id>           # 停止某个任务的运行（不带参数则停止守护进程）
cronplus logs [id] [-n 20]   # 查看日志（--no-output 只取摘要，--run-id 精确取一条）
cronplus prune [过滤条件]     # 精确删除日志条目（--task/--status/--user/--trigger/--before/--keep，--dry-run 仅统计）
cronplus reload              # 热加载配置（无需重启）
cronplus settings show       # 查看设置
cronplus settings set <k> <v># 修改设置项
cronplus settings reset      # 恢复默认设置
cronplus export              # 导出任务 JSON
cronplus import <file>       # 导入任务
cronplus cleanup             # 按任务保留策略压缩日志、清理残留临时文件
cronplus version             # 查看版本
```

### 手动运行特性

- 与守护进程共用同一执行器（`executor.RunCommand`）：超时、环境变量白名单、输出截断、日志写入完全一致
- 运行会出现在 `cronplus ps` 中，并受并发限制约束
- 实时流式输出（stdout + stderr 合并）
- 每次运行分配唯一 Run ID（格式 `#<任务号>-<用户>-manual-xxxx`）
- Ctrl+C 向子进程组发送 SIGTERM（5 秒后 SIGKILL），并照常写入日志
- 超时自动终止（SIGTERM → 5秒后 SIGKILL）
- 执行结果自动写入任务日志（唯一写入者，WebUI 不再自行拼装日志）
- **退出码透传**：任务失败时 `cronplus run` 返回任务自身的退出码，可直接用于脚本判断
- **不自动重试**：手动运行只跑一次；重试策略（`max_retries`）只作用于计划任务

### 命令行为约定

- `cronplus logs <id>`：任务不存在时报错退出（1），任务存在但未运行过时输出 `No logs`
- `cronplus prune --before/--after`：接受 `YYYY-MM-DD HH:MM:SS`、`YYYY-MM-DDTHH:MM:SS` 或 `YYYY-MM-DD`，格式非法直接报错；没有过滤条件时拒绝执行
- `cronplus reload`：优先通过控制 socket 通知守护进程，无 systemd 的环境（容器、手工启动）同样可用，最后回退到 `SIGHUP`

## Web UI（Cockpit 插件）

- **界面语言** — 默认跟随 Cockpit 会话语言（此前被忽略，只按浏览器语言判断），可在设置里固定为 9 种语言之一
- **任务管理** — 增删改查、启用/禁用、搜索过滤、分页（`taskPageSize`）
- **手动运行** — 独立弹窗，运行/停止按钮切换，实时输出，复制按钮；实际由 `cronplus run` 执行
- **运行状态** — 任务卡片显示正在运行的标记与停止按钮（数据来自 `cronplus ps`）
- **任务日志** — 分页、多维筛选（任务/用户/触发方式/状态/日期）、展开时才加载输出
- **日志清理** — 按天数/任务/状态/用户/保留条数精确删除，预览计数由 `cronplus prune --dry-run` 给出
- **服务日志** — 实时刷新、级别过滤、行数限制、清理日志
- **任务配置** — JSON 原始编辑器
- **锚点编辑** — 编辑首次预测运行时间作为步进模式（`*/N`）的锚点，自动同步固定 cron 字段
- **9 种语言** — 中文、English、日本語、한국어、Français、Deutsch、Español、Русский、Português
- **主题** — 深色 / 浅色 / 跟随系统
- **SVG 图标** — 所有 UI 图标使用矢量图形，任意分辨率下清晰渲染

## 任务配置

配置文件：`/opt/cronplus/tasks.conf`（JSON 数组）

```json
[
  {
    "id": 1,
    "title": "每日备份",
    "command": "bash /opt/backup.sh",
    "schedule": "0 0 2 * * *",
    "enabled": true,
    "run_user": "root",
    "timeout": 3600,
    "max_retries": 3,
    "retry_interval": 60,
    "env_vars": {"PATH": "/usr/local/bin:/usr/bin:/bin"},
    "cwd": "/opt"
  }
]
```

### 调度格式

6 字段 cron（含秒）：`秒 分 时 日 月 周`

```
0 * * * * *        每分钟
0 */5 * * * *      每5分钟
0 0 2 * * *        每天凌晨2点
0 0 0 1 * *        每月1号
0 0 9 * * 1-5      工作日9点
*/30 * * * * *     每30秒
```

特殊调度：

```
@reboot             系统启动时执行（支持 reboot_delay 设置延迟秒数）
```

### 锚点基准时间

对于步进模式（`*/N`），首次预测运行时间可以编辑作为锚点。后续运行时间从 `锚点 + N × 间隔` 计算，确保跨月/跨年间隔一致。

示例：`0 0 */3 * * *`（每3小时）— 编辑首次运行时间为 `2026-05-19 08:00:00`，后续运行时间将为 `11:00`、`14:00`、`17:00` 等，始终保持精确 3 小时间隔。

确认锚点时，固定的 cron 字段（纯数字如 `0 0 8 */3 * *` 中的 `8`）会自动同步以匹配编辑后的时间。

### 任务字段

| 字段 | 类型 | 说明 |
|------|------|------|
| id | int | 自动分配 |
| title | string | 任务名称 |
| command | string | 执行命令（支持 base64 编码） |
| schedule | string | cron 表达式或 @reboot |
| enabled | bool | 是否启用 |
| run_user | string | 运行用户，默认 root |
| cwd | string | 工作目录 |
| timeout | int | 超时秒数，0=不限 |
| max_retries | int | 失败重试次数 |
| retry_interval | int | 重试间隔秒数 |
| max_concurrent | int | 最大并发数 |
| kill_previous | bool | 新触发时杀掉上一次实例 |
| max_runs | int | 运行次数上限，达到后自动关闭，0=不限 |
| env_vars | map | 环境变量 |
| tags | string | 标签（逗号分隔） |
| comment | string | 任务备注 |
| log_retention_days | int | 日志保留天数，0=不限 |
| log_max_entries | int | 每任务最大日志条数，0=不限 |
| reboot_delay | int | @reboot 延迟秒数 |
| cron_base_time | string | 步进模式锚点时间，格式 `YYYY-MM-DD HH:mm:ss` |

## Run ID 追踪

每次任务执行分配唯一标识 `YYMMDD-HHMMSS-xxxx`，贯穿整个生命周期：

```
# 守护进程日志
[260423-210030-a1f2] Task [1] 每日备份 — started [auto]
[260423-210030-a1f2] Task [1] 每日备份 — ✓ success (1.2s, exit=0, attempt=1) [auto]

# CLI 输出
▶ [1] 每日备份 [manual] run=260423-210030-a1f2
...
✓ [success] 每日备份 (exit=0, 1.2s) [manual] run=260423-210030-a1f2
```

## 日志

- 守护进程日志：`/opt/cronplus/logs/cronplus.log`（自动轮转，可配置大小和备份数）
- 任务日志：`/opt/cronplus/logs/task_{id}.json`（每任务独立，追加写入的 JSONL，超过单文件大小上限时自动轮转）

> 任务日志自 2.4.8 起为 JSONL（一行一条）。旧版的 JSON 数组文件仍可正常读取，
> 首次追加时会自动迁移为 JSONL。追加是 `O_APPEND` 写一行，不再“读全量 → 改数组 →
> 重写文件”，因此高频任务不会反复重写整个历史。

### 日志轮转

守护进程日志在启动时检查文件大小，超限自动轮转：

```
cronplus.log      当前日志
cronplus.log.1    上一轮转
cronplus.log.2    更早
```

任务日志按同样规则轮转（写入时检查）：

```
task_3.json       当前日志（最新）
task_3.json.1     上一轮转
task_3.json.2     更早
```

轮转出的分段仍会被日志界面读取，因此轮转只限制单个文件大小，不会丢掉历史记录。
WebUI 拉取日志时按任务各取最近 50 条（`cronplus logs --all --per-task 50 --no-output`），
所以某个高频任务不会把其他任务的日志挤出列表。`--no-output` 让列表只带摘要，
单条输出在展开时用 `--run-id` 单独取回，避免一次刷新传输几十 MB。

可在设置中配置：
- 单文件大小上限（1/5/10/20/50 MB）
- 保留备份数（1/3/5/10）

单条运行的输出上限为 256 KB。超限时保留开头 64 KB 与结尾部分，并在中间插入
`[cronplus] ---- output truncated ----` 标记，末尾始终追加一行
`[cronplus] result: <status> (exit=<code>, <时长>)`，因此输出被截断时也能判断运行结果。

## 全局设置

配置文件：`/opt/cronplus/settings.json`

```json
{
  "language": "en",
  "theme": "auto",
  "autoRefreshInterval": 15,
  "defaultRunUser": "root",
  "defaultTimeout": 0,
  "defaultMaxRetries": 0,
  "defaultRetryInterval": 60,
  "logPageSize": 20,
  "taskPageSize": 20,
  "maxConcurrentTasks": 50,
  "logMaxBytes": 10485760,
  "logBackupCount": 5,
  "daemonLogLevel": "all",
  "daemonLogLines": 100,
  "daemonLogInterval": 2,
  "daemonLogMaxBytes": 10485760,
  "daemonLogBackupCount": 3
}
```

| 设置项 | 说明 |
| --- | --- |
| `maxConcurrentTasks` | 守护进程全局并发上限（默认 50），达到上限的新调度会被跳过并记录日志 |
| `taskPageSize` | WebUI 任务列表每页条数（默认 20） |
| `logPageSize` | WebUI 日志列表每页条数（默认 20） |

## 信号

```bash
kill -HUP <pid>     仅热加载配置（不会中断正在运行的任务）
kill -TERM <pid>    优雅关闭
kill -USR1 <pid>    输出运行状态到日志
kill -USR2 <pid>    终止所有正在运行的任务
```

> 2.4.8 之前 `SIGHUP` 会杀掉全部运行中的任务，因此“保存任务”这类热加载操作会误杀
> 正在跑的任务。现在热加载与终止彻底分开，WebUI 保存任务只会触发热加载。

## 目录结构

```
cockpit-cronplus/
├── build-deb.sh              打包脚本
├── VERSION                   版本文件
├── cockpit-cronplus/         Cockpit WebUI 插件
│   ├── index.html
│   ├── manifest.json
│   ├── lang/                 多语言（9 种）
│   └── static/               CSS + JS（SVG 矢量图标）
└── cronplus/                 Go 源码
    ├── cmd/
    │   ├── cronplus/         CLI 入口
    │   └── cronplusd/        守护进程入口
    ├── internal/
    │   ├── executor/         进程执行器（Run ID、信号转发）
    │   ├── scheduler/        Cron 调度器（锚点步进间隔）
    │   └── store/            文件存储层（原子写入）
    ├── pkg/model/            数据模型
    ├── cronplus.service      systemd unit
    ├── Makefile
    └── go.mod
```

## 构建

```bash
# 需要 Go 1.23+
cd cronplus && make build

# 打包 deb（输出到 dist/）
bash build-deb.sh [amd64|arm64]
```

### GitHub Actions

项目包含 CI 工作流（`build.yml`）：
- 支持手动输入版本号（留空则读取 `VERSION` 文件）
- 支持输入更新日志内容，发布时自动附加到 Release
- 构建 amd64 和 arm64 双架构
- 自动打包 `.deb` 文件
- 创建 GitHub Release 并附带校验和与更新日志

## License

MIT
