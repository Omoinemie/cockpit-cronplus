# cockpit-cronplus

**Gerenciador visual de tarefas cron para Cockpit** — Substitua o crontab por agendamento com precisão de segundos, controle de timeout, retry automático, limites de concorrência, logs de execução e uma interface multilíngue.

> 🌐 [中文](README.md) · [English](README_en.md) · [日本語](README_ja.md) · [한국어](README_ko.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Русский](README_ru.md)

## Funcionalidades

- Agendamento cron com precisão de segundos (6 campos: seg/min/hora/dia/mês/dia sem.)
- Auto-kill no timeout, auto-retry no erro, controle de concorrência
- Variáveis de ambiente, diretório de trabalho, retenção de logs por tarefa
- Cockpit Web UI: editor, predefinições, previsão de execuções, execução manual
- Logs de tarefa: comando/saída divididos, destaque de sintaxe, múltiplos filtros, limpeza em massa
- Logs do daemon: exibição em tempo real, destaque de palavras-chave
- 9 idiomas, tema escuro/claro, totalmente responsivo
- Ferramenta CLI: `cronplus status|list|run|logs|reload`

## Estrutura do projeto

```
cockpit-cronplus/
├── VERSION                  Versão (auto-incrementada no build)
├── build-deb.sh             Build com um clique (frontend + backend)
├── .github/workflows/       GitHub Actions build manual
├── daemon/                  Backend (daemon Python + CLI)
│   ├── src/                 Código-fonte Python
│   └── systemd/             Arquivo de serviço systemd
└── webui/                   Frontend (plugin Cockpit)
    ├── index.html
    ├── manifest.json
    ├── lang/                9 pacotes de idioma
    └── static/              CSS + JS
```

## Instalação

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

Após a instalação, encontre **Cronplus** na barra lateral do Cockpit.

## Build

```bash
./build-deb.sh    # Ler VERSION → build → auto patch+1
```

Ou acione manualmente pelo GitHub Actions — escolha patch/minor/major para build automático e criação de Release.

## Arquivos

| Caminho | Descrição |
|---------|-----------|
| `/opt/cronplus/tasks.conf` | Configuração de tarefas (JSON) |
| `/opt/cronplus/settings.json` | Configurações globais |
| `/opt/cronplus/logs/logs.json` | Logs de execução |
| `/opt/cronplus/logs/cronplus.log` | Log do daemon (rotação automática) |
| `/usr/bin/cronplus` | Ferramenta CLI |

## License

MIT
