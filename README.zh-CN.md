# Cronplus

高级 Cron 任务管理器 — Go + Cockpit WebUI 构建。

[English](README.md)

## 架构

```
cronplusd (守护进程)
├── Scheduler (调度器)      解析 cron 表达式，触发任务
└── Executor Pool (执行器)  每任务独立进程，超时/重试/日志

cronplus (CLI)              命令行管理工具（实时流式输出，信号转发）
```

## 特性

- **6 字段 cron** 支持秒级精度（`秒 分 时 日 月 周`）
- **每任务进程隔离** — 独立进程组，互不干扰
- **超时与重试** — SIGTERM → SIGKILL 递进终止，可配置重试次数与间隔
- **Run ID 追踪** — 每次执行分配唯一 ID（`YYMMDD-HHMMSS-xxxx`），全链路可追溯
- **手动运行** — 实时流式输出，Ctrl+C 终止，信号自动转发到子进程组
- **多用户支持** — 可以任意系统用户身份运行任务
- **Cockpit WebUI** — 任务管理、日志查看、服务日志、JSON 编辑器、9 种语言、深色/浅色主题

## 安装

```bash
sudo dpkg -i cronplus_<version>_<arch>.deb
sudo dpkg -i cockpit-cronplus_<version>_<arch>.deb
```

安装后守护进程自动启动。

## CLI 使用

```bash
cronplus status              # 查看状态
cronplus list                # 列出任务
cronplus run <id>            # 手动执行（实时输出，Ctrl+C 可终止）
cronplus logs [id] [-n 20]   # 查看日志
cronplus reload              # 热加载配置（无需重启）
cronplus settings show       # 查看设置
cronplus settings set <k> <v># 修改设置项
cronplus settings reset      # 恢复默认设置
cronplus export              # 导出任务 JSON
cronplus import <file>       # 导入任务
cronplus cleanup             # 清理僵死进程
cronplus version             # 查看版本
```

### 手动运行特性

- 实时流式输出（stdout + stderr）
- 每次运行分配唯一 Run ID（格式 `YYMMDD-HHMMSS-xxxx`）
- Ctrl+C 发送 SIGTERM 到子进程组
- 超时自动终止（SIGTERM → 5秒后 SIGKILL）
- 执行结果自动写入任务日志

## Web UI（Cockpit 插件）

- **任务管理** — 增删改查、启用/禁用、搜索过滤
- **手动运行** — 独立弹窗，运行/停止按钮切换，实时输出，复制按钮
- **任务日志** — 分页、多维筛选（任务/用户/触发方式/状态/日期）
- **服务日志** — 实时刷新、级别过滤、行数限制、清理日志
- **任务配置** — JSON 原始编辑器
- **9 种语言** — 中文、English、日本語、한국어、Français、Deutsch、Español、Русский、Português
- **主题** — 深色 / 浅色 / 跟随系统

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
| env_vars | map | 环境变量 |
| tags | string | 标签（逗号分隔） |
| log_retention_days | int | 日志保留天数，0=不限 |
| log_max_entries | int | 每任务最大日志条数，0=不限 |
| reboot_delay | int | @reboot 延迟秒数 |

## Run ID 追踪

每次任务执行分配唯一标识 `YYMMDD-HHMMSS-xxxx`，贯穿整个生命周期：

```
# 守护进程日志
[260423-210030-a1f2] Task [1] 每日备份 — started [auto]
[260423-210030-a1f2] Task [1] 每日备份 — ✓ success (1234ms, exit=0, attempt=1) [auto]

# CLI 输出
▶ [1] 每日备份 [manual] run=260423-210030-a1f2
...
✓ [success] 每日备份 (exit=0, 1234ms) [manual] run=260423-210030-a1f2
```

## 日志

- 守护进程日志：`/opt/cronplus/logs/cronplus.log`（自动轮转，可配置大小和备份数）
- 任务日志：`/opt/cronplus/logs/task_{id}.json`（每任务独立，最多 1000 条）

### 日志轮转

守护进程日志在启动时检查文件大小，超限自动轮转：

```
cronplus.log      当前日志
cronplus.log.1    上一轮转
cronplus.log.2    更早
```

可在设置中配置：
- 单文件大小上限（1/5/10/20/50 MB）
- 保留备份数（1/3/5/10）

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
  "logMaxBytes": 10485760,
  "logBackupCount": 5,
  "daemonLogLevel": "all",
  "daemonLogLines": 100,
  "daemonLogInterval": 2,
  "daemonLogMaxBytes": 10485760,
  "daemonLogBackupCount": 3
}
```

## 信号

```bash
kill -HUP <pid>     热加载配置，清理僵死进程
kill -TERM <pid>    优雅关闭
kill -USR1 <pid>    输出运行状态到日志
```

## 目录结构

```
cockpit-cronplus/
├── build-deb.sh              打包脚本
├── VERSION                   版本文件
├── cockpit-cronplus/         Cockpit WebUI 插件
│   ├── index.html
│   ├── manifest.json
│   ├── lang/                 多语言（9 种）
│   └── static/               CSS + JS
└── cronplus/                 Go 源码
    ├── cmd/
    │   ├── cronplus/         CLI 入口
    │   └── cronplusd/        守护进程入口
    ├── internal/
    │   ├── executor/         进程执行器（Run ID、信号转发）
    │   ├── scheduler/        Cron 调度器
    │   └── store/            文件存储层
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
- 构建 amd64 和 arm64 双架构
- 自动打包 `.deb` 文件
- 创建 GitHub Release 并附带校验和

## License

MIT
