# cockpit-cronplus

**Визуальный менеджер cron-задач для Cockpit** — Замените crontab на планирование с точностью до секунды, контроль таймаута, автоповтор, ограничение параллельности, журналы выполнения и многоязычный интерфейс.

> 🌐 [中文](README.md) · [English](README_en.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Português](README_pt-BR.md)

## Возможности

- Планирование cron с точностью до секунды (6 полей: сек/мин/час/день/мес/день нед.)
- Автозавершение по таймауту, автоповтор при ошибке, контроль параллельности
- Переменные окружения, рабочий каталог, хранение журналов на задачу
- Cockpit Web UI: редактор, пресеты расписания, прогноз запусков, ручное выполнение
- Журналы задач: команда/вывод, подсветка синтаксиса, множественные фильтры, массовая очистка
- Журналы демона: отображение в реальном времени, подсветка ключевых слов
- 9 языков, тёмная/светлая тема, полная адаптация для мобильных
- CLI: `cronplus status|list|run|logs|reload`

## Структура проекта

```
cockpit-cronplus/
├── VERSION                  Версия (автоинкремент при сборке)
├── build-deb.sh             Сборка в один клик (фронтенд + бэкенд)
├── .github/workflows/       GitHub Actions ручная сборка
├── daemon/                  Бэкенд (демон Python + CLI)
│   ├── src/                 Исходный код Python
│   └── systemd/             Файл службы systemd
└── webui/                   Фронтенд (плагин Cockpit)
    ├── index.html
    ├── manifest.json
    ├── lang/                9 языковых пакетов
    └── static/              CSS + JS
```

## Установка

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

После установки найдите **Cronplus** в боковой панели Cockpit.

## Сборка

```bash
./build-deb.sh    # Чтение VERSION → сборка → авто patch+1
```

Или запустите вручную из GitHub Actions — выберите patch/minor/major для автосборки и создания релиза.

## Файлы

| Путь | Описание |
|------|----------|
| `/opt/cronplus/tasks.conf` | Конфигурация задач (JSON) |
| `/opt/cronplus/settings.json` | Глобальные настройки |
| `/opt/cronplus/logs/logs.json` | Журналы выполнения |
| `/opt/cronplus/logs/cronplus.log` | Журнал демона (авторотация) |
| `/usr/bin/cronplus` | CLI-утилита |

## License

MIT
