# cockpit-cronplus

**基于 Cockpit 的可视化定时任务管理器** — 替代传统 crontab，提供秒级精度调度、超时控制、失败重试、并发限制、执行日志、多语言界面，开箱即用。

> 🌐 [English](README_en.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## 功能

- 秒级 cron 调度（6 字段：秒/分/时/日/月/周）
- 执行超时自动终止、失败自动重试、并发实例控制
- 每任务独立环境变量、工作目录、日志保留策略
- Cockpit Web UI：任务编辑器、调度预设、下次运行预测、手动执行
- 任务日志：命令/输出分栏、语法高亮、多维筛选、一键清理
- 服务日志：实时查看 daemon 运行日志，关键字高亮
- 9 种语言界面，暗色/亮色主题，完整移动端适配
- CLI 工具：`cronplus status|list|run|logs|reload`

## 项目结构

```
cockpit-cronplus/
├── VERSION                  版本号（build 自动递增）
├── build-deb.sh             一键打包（前后端合并）
├── .github/workflows/       GitHub Actions 手动打包
├── daemon/                  后端（Python daemon + CLI）
│   ├── src/                 Python 源码
│   └── systemd/             systemd 服务文件
└── webui/                   前端（Cockpit 插件）
    ├── index.html
    ├── manifest.json
    ├── lang/                9 种语言包
    └── static/              CSS + JS 模块
```

## 安装

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

安装后在 Cockpit 左侧菜单找到 **Cronplus**。

## 构建

```bash
./build-deb.sh    # 读取 VERSION → 打包 → 自动 patch+1
```

或在 GitHub Actions 页面手动触发，选择 patch/minor/major 自动打包并创建 Release。

## 文件

| 路径 | 说明 |
|------|------|
| `/opt/cronplus/tasks.conf` | 任务配置（JSON） |
| `/opt/cronplus/settings.json` | 全局设置 |
| `/opt/cronplus/logs/logs.json` | 执行日志 |
| `/opt/cronplus/logs/cronplus.log` | daemon 日志（自动轮转） |
| `/usr/bin/cronplus` | CLI 工具 |

## License

MIT
