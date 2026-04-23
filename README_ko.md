# cockpit-cronplus

**Cockpit용 비주얼 cron 작업 관리자** — 기존 crontab을 대체하여 초 단위 스케줄링, 타임아웃 제어, 자동 재시도, 동시성 제한, 실행 로그, 다국어 UI를 제공합니다.

> 🌐 [中文](README.md) · [English](README_en.md) · [日本語](README_ja.md) · [Français](README_fr.md) · [Deutsch](README_de.md) · [Español](README_es.md) · [Русский](README_ru.md) · [Português](README_pt-BR.md)

## 기능

- 초 단위 cron 스케줄링 (6 필드: 초/분/시/일/월/요일)
- 타임아웃 시 자동 종료, 실패 시 자동 재시도, 동시성 제어
- 작업별 환경 변수, 작업 디렉토리, 로그 보관 설정
- Cockpit Web UI: 작업 편집기, 스케줄 프리셋, 다음 실행 미리보기, 수동 실행
- 작업 로그: 명령어/출력 분할 표시, 구문 강조, 다차원 필터, 일괄 정리
- 데몬 로그: 실시간 표시, 키워드 강조
- 9개 언어, 다크/라이트 테마, 모바일 완전 대응
- CLI 도구: `cronplus status|list|run|logs|reload`

## 프로젝트 구조

```
cockpit-cronplus/
├── VERSION                  버전 (빌드 시 자동 증가)
├── build-deb.sh             원클릭 빌드 (프론트엔드 + 백엔드)
├── .github/workflows/       GitHub Actions 수동 빌드
├── daemon/                  백엔드 (Python daemon + CLI)
│   ├── src/                 Python 소스
│   └── systemd/             systemd 서비스 파일
└── webui/                   프론트엔드 (Cockpit 플러그인)
    ├── index.html
    ├── manifest.json
    ├── lang/                9개 언어팩
    └── static/              CSS + JS 모듈
```

## 설치

```bash
sudo dpkg -i cronplus_<version>_all.deb
sudo dpkg -i cockpit-cronplus_<version>_all.deb
```

설치 후 Cockpit 사이드바에서 **Cronplus**를 찾으세요.

## 빌드

```bash
./build-deb.sh    # VERSION 읽기 → 빌드 → 자동 patch+1
```

GitHub Actions에서 수동 트리거 가능 — patch/minor/major를 선택하여 자동 빌드 및 릴리스 생성.

## 파일

| 경로 | 설명 |
|------|------|
| `/opt/cronplus/tasks.conf` | 작업 설정 (JSON) |
| `/opt/cronplus/settings.json` | 전역 설정 |
| `/opt/cronplus/logs/logs.json` | 실행 로그 |
| `/opt/cronplus/logs/cronplus.log` | 데몬 로그 (자동 로테이션) |
| `/usr/bin/cronplus` | CLI 도구 |

## License

MIT
