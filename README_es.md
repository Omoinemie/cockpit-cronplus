# cockpit-cronplus

**Gestor visual de tareas cron para Cockpit** — Reemplace crontab con programación por segundo, control de timeout, reintento automático, límites de concurrencia, registros de ejecución y una interfaz multilingüe.

> 🌐 [中文](README.md) · [English](README_en.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## Características

- Programación cron por segundo (6 campos: seg/min/hora/día/mes/día sem.)
- Auto-kill en timeout, auto-retry en error, control de concurrencia
- Variables de entorno, directorio de trabajo, retención de logs por tarea
- Cockpit Web UI: editor, preajustes, previsión de ejecuciones, ejecución manual
- Logs de tarea: vista comando/salida, resaltado de sintaxis, filtros múltiples, limpieza masiva
- Logs del daemon: visualización en tiempo real, resaltado de palabras clave
- 9 idiomas, tema oscuro/claro, completamente responsive
- Herramienta CLI: `cronplus status|list|run|logs|reload`

## Estructura del proyecto

```
cockpit-cronplus/
├── VERSION                  Versión (auto-incrementada al construir)
├── build-deb.sh             Build con un clic (frontend + backend)
├── .github/workflows/       GitHub Actions build manual
├── daemon/                  Backend (daemon Python + CLI)
│   ├── src/                 Código fuente Python
│   └── systemd/             Archivo de servicio systemd
└── webui/                   Frontend (plugin Cockpit)
    ├── index.html
    ├── manifest.json
    ├── lang/                9 paquetes de idioma
    └── static/              CSS + JS
```

## Instalación

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

Después de instalar, encuentre **Cronplus** en la barra lateral de Cockpit.

## Build

```bash
./build-deb.sh    # Leer VERSION → build → auto patch+1
```

O active manualmente desde GitHub Actions — elija patch/minor/major para build automático y Release.

## Archivos

| Ruta | Descripción |
|------|-------------|
| `/opt/cronplus/tasks.conf` | Configuración de tareas (JSON) |
| `/opt/cronplus/settings.json` | Configuración global |
| `/opt/cronplus/logs/logs.json` | Logs de ejecución |
| `/opt/cronplus/logs/cronplus.log` | Log del daemon (rotación automática) |
| `/usr/bin/cronplus` | Herramienta CLI |

## License

MIT
