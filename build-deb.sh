#!/bin/bash
set -euo pipefail

# cockpit-cronplus — one-click build both .deb packages
# Usage: ./build-deb.sh
#
# Reads version from ./VERSION, uses it for the build,
# then auto-increments the patch number and writes back.

DIR="$(cd "$(dirname "$0")" && pwd)"
VERSION_FILE="${DIR}/VERSION"

# ── Read current version ──
if [ ! -f "${VERSION_FILE}" ]; then
    echo "1.0.0" > "${VERSION_FILE}"
fi
VERSION="$(cat "${VERSION_FILE}" | tr -d '[:space:]')"

if [ -z "${VERSION}" ]; then
    echo "ERROR: VERSION file is empty"
    exit 1
fi

echo "========================================"
echo "  Building cockpit-cronplus ${VERSION}"
echo "========================================"

# ──────────────────────────────────────────
# 1. Backend: cronplus_${VERSION}_all.deb
# ──────────────────────────────────────────

PKGNAME="cronplus"
ARCH="all"
PKGDIR="${PKGNAME}_${VERSION}_${ARCH}"
SRC="${DIR}/daemon"

echo ""
echo "=== [1/2] Building ${PKGNAME}_${VERSION}_${ARCH}.deb ==="

rm -rf "${DIR:?}/${PKGDIR}" "${DIR}/${PKGNAME}_"*.deb

mkdir -p "${PKGDIR}/DEBIAN"
mkdir -p "${PKGDIR}/opt/cronplus/src"
mkdir -p "${PKGDIR}/opt/cronplus/logs"
mkdir -p "${PKGDIR}/etc/systemd/system"
mkdir -p "${PKGDIR}/usr/bin"

# Source files
cp "${SRC}/src/"*.py "${PKGDIR}/opt/cronplus/src/"

# Systemd service
cp "${SRC}/systemd/cronplus.service" "${PKGDIR}/etc/systemd/system/"

# Default empty config
echo '[]' > "${PKGDIR}/opt/cronplus/tasks.conf"

# Default settings.json
cat > "${PKGDIR}/opt/cronplus/settings.json" << 'SETTINGS'
{
  "language": "zh-CN",
  "theme": "auto",
  "autoRefreshInterval": 15,
  "logMaxBytes": 10485760,
  "logBackupCount": 5,
  "defaultRunUser": "root",
  "defaultTimeout": 0,
  "defaultMaxRetries": 0,
  "defaultRetryInterval": 60,
  "logPageSize": 20,
  "taskPageSize": 20,
  "daemonLogLevel": "all",
  "daemonLogLines": 100,
  "daemonLogInterval": 2
}
SETTINGS

# CLI wrapper
cat > "${PKGDIR}/usr/bin/cronplus" << 'EOF'
#!/bin/bash
exec python3 /opt/cronplus/src/cli.py "$@"
EOF
chmod 755 "${PKGDIR}/usr/bin/cronplus"

# DEBIAN/control
cat > "${PKGDIR}/DEBIAN/control" <<EOF
Package: ${PKGNAME}
Version: ${VERSION}
Architecture: ${ARCH}
Maintainer: Cronplus <admin@example.com>
Depends: python3 (>= 3.9)
Section: admin
Priority: optional
Description: Advanced cron task manager daemon
 Daemon that replaces crontab with: timeout control, retry on failure,
 per-task environment variables, concurrency limits, JSON config,
 execution logs with output capture, zombie process cleanup,
 drift-compensated scheduling, and log rotation. Includes CLI tool.
EOF

# postinst
cat > "${PKGDIR}/DEBIAN/postinst" << POSTINST
#!/bin/bash
set -e
case "\$1" in
    configure)
        mkdir -p /opt/cronplus/logs
        chmod 755 /opt/cronplus /opt/cronplus/logs
        if [ ! -f /opt/cronplus/tasks.conf ]; then
            echo '[]' > /opt/cronplus/tasks.conf
        fi
        if [ ! -f /opt/cronplus/settings.json ]; then
            cat > /opt/cronplus/settings.json << 'SETTINGS'
{
  "language": "zh-CN",
  "theme": "auto",
  "autoRefreshInterval": 15,
  "logMaxBytes": 10485760,
  "logBackupCount": 5,
  "defaultRunUser": "root",
  "defaultTimeout": 0,
  "defaultMaxRetries": 0,
  "defaultRetryInterval": 60,
  "logPageSize": 20,
  "taskPageSize": 20,
  "daemonLogLevel": "all",
  "daemonLogLines": 100,
  "daemonLogInterval": 2
}
SETTINGS
        fi
        systemctl daemon-reload 2>/dev/null || true
        if systemctl is-enabled cronplus.service >/dev/null 2>&1; then
            systemctl restart cronplus.service 2>/dev/null || true
        else
            systemctl enable cronplus.service 2>/dev/null || true
            systemctl start cronplus.service 2>/dev/null || true
        fi
        echo ""
        echo "  cronplus ${VERSION} installed"
        echo "  Config:   /opt/cronplus/tasks.conf"
        echo "  Settings: /opt/cronplus/settings.json"
        echo "  Logs:     /opt/cronplus/logs/logs.json"
        echo "  CLI:      cronplus status|list|run|logs|settings|reload"
        echo ""
        ;;
esac
POSTINST

cat > "${PKGDIR}/DEBIAN/conffiles" << 'CONFFILES'
/opt/cronplus/tasks.conf
/opt/cronplus/settings.json
CONFFILES
chmod 755 "${PKGDIR}/DEBIAN/postinst"

cat > "${PKGDIR}/DEBIAN/prerm" << 'EOF'
#!/bin/bash
case "$1" in
    remove|deconfigure)
        systemctl stop cronplus.service 2>/dev/null || true
        systemctl disable cronplus.service 2>/dev/null || true
        ;;
esac
EOF
chmod 755 "${PKGDIR}/DEBIAN/prerm"

cat > "${PKGDIR}/DEBIAN/postrm" << 'EOF'
#!/bin/bash
case "$1" in
    purge) rm -rf /opt/cronplus /run/cronplus ;;
    remove) systemctl daemon-reload 2>/dev/null || true ;;
esac
EOF
chmod 755 "${PKGDIR}/DEBIAN/postrm"

find "${PKGDIR}" -type d -exec chmod 755 {} \;
find "${PKGDIR}" -type f -exec chmod 644 {} \;
chmod 755 "${PKGDIR}/DEBIAN/postinst" "${PKGDIR}/DEBIAN/prerm" "${PKGDIR}/DEBIAN/postrm"
chmod 755 "${PKGDIR}/usr/bin/cronplus"

dpkg-deb --build --root-owner-group "${PKGDIR}"
[ -f "${DIR}/${PKGNAME}_${VERSION}_${ARCH}.deb" ] || mv "${PKGDIR}.deb" "${DIR}/"
echo "Built: ${DIR}/${PKGDIR}.deb  ($(du -h "${DIR}/${PKGDIR}.deb" | cut -f1))"
rm -rf "${PKGDIR}"

# ──────────────────────────────────────────
# 2. Frontend: cockpit-cronplus_${VERSION}_all.deb
# ──────────────────────────────────────────

PKGNAME="cockpit-cronplus"
PKGDIR="${PKGNAME}_${VERSION}_${ARCH}"
SRC="${DIR}/webui"

echo ""
echo "=== [2/2] Building ${PKGNAME}_${VERSION}_${ARCH}.deb ==="

rm -rf "${DIR:?}/${PKGDIR}" "${DIR}/${PKGNAME}_"*.deb

mkdir -p "${PKGDIR}/DEBIAN"
mkdir -p "${PKGDIR}/usr/share/cockpit/cronplus"

# Plugin files
cp "${SRC}/manifest.json"           "${PKGDIR}/usr/share/cockpit/cronplus/"
cp "${SRC}/index.html"              "${PKGDIR}/usr/share/cockpit/cronplus/"
cp -r "${SRC}/static"               "${PKGDIR}/usr/share/cockpit/cronplus/"
cp -r "${SRC}/lang"                 "${PKGDIR}/usr/share/cockpit/cronplus/"

# Patch manifest.json version
sed -i "s/\"plugin_version\":\s*\"[^\"]*\"/\"plugin_version\": \"${VERSION}\"/" \
    "${PKGDIR}/usr/share/cockpit/cronplus/manifest.json"

# DEBIAN/control
cat > "${PKGDIR}/DEBIAN/control" <<EOF
Package: ${PKGNAME}
Version: ${VERSION}
Architecture: ${ARCH}
Maintainer: Cronplus <admin@example.com>
Depends: cockpit (>= 276), cronplus (>= ${VERSION})
Section: admin
Priority: optional
Description: Cockpit web UI for cronplus
 Provides a visual interface for managing cronplus tasks.
 Features: task editor with schedule presets, next-run preview,
 manual execution, execution logs with advanced filtering/cleanup,
 raw config editor, import/export JSON, settings panel.
 Requires cronplus backend daemon.
EOF

cat > "${PKGDIR}/DEBIAN/postinst" << 'POSTINST'
#!/bin/bash
set -e
case "$1" in
    configure)
        systemctl is-active --quiet cockpit.socket 2>/dev/null && \
            systemctl restart cockpit.socket 2>/dev/null || true
        echo "  cockpit-cronplus installed — Open Cockpit → Cronplus"
        ;;
esac
POSTINST
chmod 755 "${PKGDIR}/DEBIAN/postinst"

cat > "${PKGDIR}/DEBIAN/prerm" << 'EOF'
#!/bin/bash
case "$1" in
    remove|deconfigure)
        systemctl is-active --quiet cockpit.socket 2>/dev/null && \
            systemctl restart cockpit.socket 2>/dev/null || true
        ;;
esac
EOF
chmod 755 "${PKGDIR}/DEBIAN/prerm"

cat > "${PKGDIR}/DEBIAN/postrm" << 'EOF'
#!/bin/bash
EOF
chmod 755 "${PKGDIR}/DEBIAN/postrm"

find "${PKGDIR}" -type d -exec chmod 755 {} \;
find "${PKGDIR}" -type f -exec chmod 644 {} \;
chmod 755 "${PKGDIR}/DEBIAN/postinst" "${PKGDIR}/DEBIAN/prerm" "${PKGDIR}/DEBIAN/postrm"

dpkg-deb --build --root-owner-group "${PKGDIR}"
[ -f "${DIR}/${PKGNAME}_${VERSION}_${ARCH}.deb" ] || mv "${PKGDIR}.deb" "${DIR}/"
echo "Built: ${DIR}/${PKGDIR}.deb  ($(du -h "${DIR}/${PKGDIR}.deb" | cut -f1))"
rm -rf "${PKGDIR}"

# ──────────────────────────────────────────
# 3. Auto-increment patch version
# ──────────────────────────────────────────

MAJOR="$(echo "${VERSION}" | cut -d. -f1)"
MINOR="$(echo "${VERSION}" | cut -d. -f2)"
PATCH="$(echo "${VERSION}" | cut -d. -f3)"
NEW_PATCH=$((PATCH + 1))
NEW_VERSION="${MAJOR}.${MINOR}.${NEW_PATCH}"

echo "${NEW_VERSION}" > "${VERSION_FILE}"

echo ""
echo "========================================"
echo "  Build complete! v${VERSION}"
echo "  Next version: v${NEW_VERSION}"
echo "========================================"
echo ""
ls -lh "${DIR}/cronplus_${VERSION}_all.deb" "${DIR}/cockpit-cronplus_${VERSION}_all.deb"
echo ""
echo "Install:"
echo "  sudo dpkg -i ${DIR}/cronplus_${VERSION}_all.deb ${DIR}/cockpit-cronplus_${VERSION}_all.deb"
