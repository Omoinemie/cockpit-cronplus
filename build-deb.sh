#!/bin/bash
set -euo pipefail

# cockpit-cronplus — one-click build both .deb packages
# Usage: ./build-deb.sh

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

# ── Sync version to all source files ──
echo "Syncing version ${VERSION} to source files..."
[ -f "${DIR}/daemon/src/version.py" ] && sed -i "s/^VERSION = .*/VERSION = '${VERSION}'/" "${DIR}/daemon/src/version.py"
[ -f "${DIR}/webui/manifest.json" ] && sed -i "s/\"plugin_version\":\s*\"[^\"]*\"/\"plugin_version\": \"${VERSION}\"/" "${DIR}/webui/manifest.json"

RELEASE_DIR="${DIR}/release"
mkdir -p "${RELEASE_DIR}"

echo "========================================"
echo "  Building cockpit-cronplus ${VERSION}"
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

rm -rf "${DIR:?}/${PKGDIR}" "${RELEASE_DIR}/${PKGNAME}_"*.deb

# 构造目录结构
mkdir -p "${PKGDIR}/DEBIAN"
mkdir -p "${PKGDIR}/opt/cronplus/src"
mkdir -p "${PKGDIR}/opt/cronplus/logs"
mkdir -p "${PKGDIR}/etc/systemd/system"
mkdir -p "${PKGDIR}/usr/bin"

# 复制源码
cp "${SRC}/src/"*.py "${PKGDIR}/opt/cronplus/src/"
cp "${SRC}/systemd/cronplus.service" "${PKGDIR}/etc/systemd/system/"

# 写入配置文件
echo '[]' > "${PKGDIR}/opt/cronplus/tasks.conf"
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
EOF

# DEBIAN/conffiles
cat > "${PKGDIR}/DEBIAN/conffiles" << 'CONFFILES'
/opt/cronplus/tasks.conf
/opt/cronplus/settings.json
CONFFILES

# DEBIAN/postinst
cat > "${PKGDIR}/DEBIAN/postinst" << POSTINST
#!/bin/bash
set -e
case "\$1" in
    configure)
        mkdir -p /opt/cronplus/logs
        chmod 755 /opt/cronplus /opt/cronplus/logs
        systemctl daemon-reload 2>/dev/null || true
        if systemctl is-enabled cronplus.service >/dev/null 2>&1; then
            systemctl restart cronplus.service 2>/dev/null || true
        else
            systemctl enable cronplus.service 2>/dev/null || true
            systemctl start cronplus.service 2>/dev/null || true
        fi
        ;;
esac
POSTINST

# DEBIAN/prerm
cat > "${PKGDIR}/DEBIAN/prerm" << 'EOF'
#!/bin/bash
case "$1" in
    remove|deconfigure)
        systemctl stop cronplus.service 2>/dev/null || true
        systemctl disable cronplus.service 2>/dev/null || true
        ;;
esac
EOF

# DEBIAN/postrm (关键修复点)
cat > "${PKGDIR}/DEBIAN/postrm" << 'EOF'
#!/bin/bash
set -e
case "$1" in
    purge)
        # 绝对路径硬编码，防止变量失效
        if [ -d "/opt/cronplus" ]; then
            rm -rf "/opt/cronplus"
        fi
        rm -rf "/run/cronplus"
        ;;
    remove|upgrade|failed-upgrade|abort-install|abort-upgrade|disappear)
        systemctl daemon-reload 2>/dev/null || true
        ;;
esac
exit 0
EOF

# 设置权限并打包（只对应用子目录设置，不触碰 /opt 等父目录）
find "${PKGDIR}/opt/cronplus" -type d -exec chmod 755 {} \;
find "${PKGDIR}/usr" -type d -exec chmod 755 {} \;
find "${PKGDIR}/opt/cronplus" -type f -exec chmod 644 {} \;
find "${PKGDIR}/usr" -type f -exec chmod 644 {} \;
chmod 755 "${PKGDIR}/DEBIAN/postinst" "${PKGDIR}/DEBIAN/prerm" "${PKGDIR}/DEBIAN/postrm"
chmod 755 "${PKGDIR}/usr/bin/cronplus"

dpkg-deb --build --root-owner-group "${PKGDIR}"
mv "${PKGDIR}.deb" "${RELEASE_DIR}/"
echo "Built: ${RELEASE_DIR}/${PKGDIR}.deb ($(du -h "${RELEASE_DIR}/${PKGDIR}.deb" | cut -f1))"
rm -rf "${PKGDIR}"

# ──────────────────────────────────────────
# 2. Frontend: cockpit-cronplus_${VERSION}_all.deb
# ──────────────────────────────────────────

PKGNAME="cockpit-cronplus"
PKGDIR="${PKGNAME}_${VERSION}_${ARCH}"
SRC="${DIR}/webui"

echo ""
echo "=== [2/2] Building ${PKGNAME}_${VERSION}_${ARCH}.deb ==="

rm -rf "${DIR:?}/${PKGDIR}" "${RELEASE_DIR}/${PKGNAME}_"*.deb

mkdir -p "${PKGDIR}/DEBIAN"
mkdir -p "${PKGDIR}/usr/share/cockpit/cronplus"

cp "${SRC}/manifest.json" "${PKGDIR}/usr/share/cockpit/cronplus/"
cp "${SRC}/index.html" "${PKGDIR}/usr/share/cockpit/cronplus/"
[ -d "${SRC}/static" ] && cp -r "${SRC}/static" "${PKGDIR}/usr/share/cockpit/cronplus/"
[ -d "${SRC}/lang" ] && cp -r "${SRC}/lang" "${PKGDIR}/usr/share/cockpit/cronplus/"

cat > "${PKGDIR}/DEBIAN/control" <<EOF
Package: ${PKGNAME}
Version: ${VERSION}
Architecture: ${ARCH}
Maintainer: Cronplus <admin@example.com>
Depends: cockpit (>= 276), cronplus (>= ${VERSION})
Section: admin
Priority: optional
Description: Cockpit web UI for cronplus
EOF

cat > "${PKGDIR}/DEBIAN/postinst" << 'POSTINST'
#!/bin/bash
set -e
if [ "$1" = "configure" ]; then
    systemctl is-active --quiet cockpit.socket && systemctl restart cockpit.socket || true
fi
POSTINST

cat > "${PKGDIR}/DEBIAN/prerm" << 'EOF'
#!/bin/bash
if [ "$1" = "remove" ]; then
    systemctl is-active --quiet cockpit.socket && systemctl restart cockpit.socket || true
fi
EOF

# Frontend postrm 保持简单
cat > "${PKGDIR}/DEBIAN/postrm" << 'EOF'
#!/bin/bash
exit 0
EOF

find "${PKGDIR}/usr" -type d -exec chmod 755 {} \;
find "${PKGDIR}/usr" -type f -exec chmod 644 {} \;
chmod 755 "${PKGDIR}/DEBIAN/postinst" "${PKGDIR}/DEBIAN/prerm" "${PKGDIR}/DEBIAN/postrm"

dpkg-deb --build --root-owner-group "${PKGDIR}"
mv "${PKGDIR}.deb" "${RELEASE_DIR}/"
echo "Built: ${RELEASE_DIR}/${PKGDIR}.deb ($(du -h "${RELEASE_DIR}/${PKGDIR}.deb" | cut -f1))"
rm -rf "${PKGDIR}"

echo ""
echo "Done! All packages are in ${RELEASE_DIR}"
