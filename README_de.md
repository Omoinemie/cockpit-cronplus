# cockpit-cronplus

**Visueller Cron-Aufgabenmanager für Cockpit** — Ersetzen Sie crontab mit Sekunden-Genauigkeit, Timeout-Steuerung, Auto-Retry, Nebenläufigkeitskontrolle, Ausführungslogs und einer mehrsprachigen Oberfläche.

> 🌐 [中文](README.md) · [English](README_en.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Español](README_es.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## Funktionen

- Cron-Planung mit Sekunden-Genauigkeit (6 Felder: Sek/Min/Stunde/Tag/Monat/Wochentag)
- Auto-Kill bei Timeout, Auto-Retry bei Fehler, Nebenläufigkeitskontrolle
- Umgebungsvariablen, Arbeitsverzeichnis, Log-Aufbewahrung pro Aufgabe
- Cockpit Web UI: Editor, Zeitplan-Vorlagen, nächste Ausführung, manuelle Ausführung
- Aufgaben-Logs: Befehl/Ausgabe geteilt, Syntax-Hervorhebung, Multi-Filter, Massenbereinigung
- Daemon-Logs: Echtzeit-Anzeige, Schlüsselwort-Hervorhebung
- 9 Sprachen, Dunkel/Hell-Theme, vollständig mobil-responsive
- CLI-Werkzeug: `cronplus status|list|run|logs|reload`

## Projektstruktur

```
cockpit-cronplus/
├── VERSION                  Version (auto-inkrementiert beim Build)
├── build-deb.sh             Ein-Klick-Build (Frontend + Backend)
├── .github/workflows/       GitHub Actions manueller Build
├── daemon/                  Backend (Python-Daemon + CLI)
│   ├── src/                 Python-Quellcode
│   └── systemd/             systemd-Service-Datei
└── webui/                   Frontend (Cockpit-Plugin)
    ├── index.html
    ├── manifest.json
    ├── lang/                9 Sprachpakete
    └── static/              CSS + JS
```

## Installation

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

Nach der Installation finden Sie **Cronplus** in der Cockpit-Seitenleiste.

## Build

```bash
./build-deb.sh    # VERSION lesen → Build → auto patch+1
```

Oder manuell über GitHub Actions auslösen — wählen Sie patch/minor/major für automatischen Build und Release.

## Dateien

| Pfad | Beschreibung |
|------|--------------|
| `/opt/cronplus/tasks.conf` | Aufgaben-Konfiguration (JSON) |
| `/opt/cronplus/settings.json` | Globale Einstellungen |
| `/opt/cronplus/logs/logs.json` | Ausführungslogs |
| `/opt/cronplus/logs/cronplus.log` | Daemon-Log (auto-rotiert) |
| `/usr/bin/cronplus` | CLI-Werkzeug |

## License

MIT
