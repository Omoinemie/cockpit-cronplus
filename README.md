# cronplus

Advanced Cron Task Manager — Go 重构版

## 架构

```
cronplusd (守护进程)
├── Scheduler (调度器)    解析 cron 配置，触发任务
└── Executor Pool (执行器) 每任务独立进程，超时/重试/日志

cronplus (CLI)            命令行管理工具（实时流式输出，信号转发）
```

## 安装

```bash
dpkg -i cronplus_2.0.7_amd64.deb           # 后端，安装后自动启动
dpkg -i cockpit-cronplus_2.0.7_amd64.deb   # Cockpit 前端插件
```

## CLI 使用

```bash
cronplus status              # 查看状态
cronplus list                # 列出任务
cronplus run <id>            # 手动执行（实时流式输出，Ctrl+C 可终止）
cronplus logs [id] [-n 20]   # 查看日志
cronplus reload              # 热加载配置
cronplus settings show       # 查看设置
cronplus export              # 导出任务 JSON
cronplus import <file>       # 导入任务
cronplus cleanup             # 清理僵死进程
```

### 手动运行特性

- 命令输出实时流式显示（stdout + stderr）
- 每次运行分配唯一 Run ID（格式 `YYMMDD-HHMMSS-xxxx`），全链路追踪
- 支持 Ctrl+C 终止，自动转发信号到子进程组
- 超时自动终止（SIGTERM → SIGKILL）
- 执行结果自动写入任务日志

## Web UI (Cockpit 插件)

- 任务管理：增删改查、启用/禁用、搜索过滤
- 手动运行：独立弹窗，运行/停止按钮切换，实时输出，右上角复制按钮
- 任务日志：分页、多维筛选（任务/用户/触发方式/状态/日期）
- 服务日志：实时刷新、级别过滤、行数限制、清理日志
- 任务配置：JSON 原始编辑器
- 多语言：中文、English、日本語、한국어、Français、Deutsch、Español、Русский、Português
- 主题：深色/浅色/跟随系统

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
@reboot             系统启动时执行（支持 reboot_delay）
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
# Daemon 日志
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
  "language": "zh-CN",
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
cronplus/
├── build-deb.sh              打包脚本
├── deb/                      deb 安装脚本
│   ├── postinst              安装后（创建目录、启动服务）
│   ├── prerm                 卸载前（停止服务）
│   └── postrm                卸载后（清理 systemd）
├── cockpit-cronplus/         Cockpit WebUI 插件
│   ├── index.html
│   ├── manifest.json
│   ├── lang/                 多语言（9 种）
│   └── static/               CSS + JS
└── cronplus/                 Go 源码
    ├── cmd/
    │   ├── cronplus/         CLI
    │   └── cronplusd/        Daemon
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
export PATH=$PATH:/usr/local/go/bin

# 编译
cd cronplus && make build

# 打包 deb（输出到项目根目录）
cd .. && bash build-deb.sh
```

## License

MIT
