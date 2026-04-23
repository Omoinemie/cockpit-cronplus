# cockpit-cronplus

**Gestionnaire de tâches cron visuel pour Cockpit** — Remplacez crontab par une planification à la seconde, un contrôle de timeout, une relance automatique, des limites de concurrence, des journaux d'exécution et une interface multilingue.

> 🌐 [中文](README.md) · [English](README_en.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## Fonctionnalités

- Planification cron à la seconde (6 champs : sec/min/heure/jour/mois/jour sem.)
- Arrêt auto sur timeout, relance auto sur erreur, contrôle de concurrence
- Variables d'environnement, répertoire de travail, rétention des journaux par tâche
- Cockpit Web UI : éditeur, préréglages, prévision des exécutions, exécution manuelle
- Journaux : vue commandes/sortie, coloration syntaxique, filtres multiples, nettoyage en masse
- Journaux daemon : affichage en temps réel, mise en évidence des mots-clés
- 9 langues, thème sombre/clair, responsive mobile complet
- Outil CLI : `cronplus status|list|run|logs|reload`

## Structure du projet

```
cockpit-cronplus/
├── VERSION                  Version (auto-incrémentée au build)
├── build-deb.sh             Build en un clic (frontend + backend)
├── .github/workflows/       GitHub Actions build manuel
├── daemon/                  Backend (daemon Python + CLI)
│   ├── src/                 Source Python
│   └── systemd/             Fichier service systemd
└── webui/                   Frontend (plugin Cockpit)
    ├── index.html
    ├── manifest.json
    ├── lang/                9 packs de langue
    └── static/              CSS + JS
```

## Installation

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

Après installation, trouvez **Cronplus** dans la barre latérale de Cockpit.

## Build

```bash
./build-deb.sh    # Lire VERSION → build → auto patch+1
```

Ou déclenchez manuellement depuis GitHub Actions — choisissez patch/minor/major pour un build automatique et une Release.

## Fichiers

| Chemin | Description |
|--------|-------------|
| `/opt/cronplus/tasks.conf` | Configuration des tâches (JSON) |
| `/opt/cronplus/settings.json` | Paramètres globaux |
| `/opt/cronplus/logs/logs.json` | Journaux d'exécution |
| `/opt/cronplus/logs/cronplus.log` | Journal du daemon (rotation auto) |
| `/usr/bin/cronplus` | Outil CLI |

## License

MIT
