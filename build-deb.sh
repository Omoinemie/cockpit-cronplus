#!/bin/bash
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$DIR/cronplus"

# ── 1. 严格从文件或环境变量读取版本 ──
VERSION="${VERSION:-$(cat "$DIR/VERSION" 2>/dev/null | tr -d '[:space:]')}"
if [ -z "$VERSION" ]; then
    echo "ERROR: VERSION file not found or empty. Please create a VERSION file."
    exit 1
fi

# ── 2. 处理架构输入 (现在的第1个参数) ──
ARCH=${1:-"amd64"}
echo "==> Target Architecture: ${ARCH}"

# 同步更新源码中的 manifest.json，保持构建一致性
MANIFEST_PATH="$DIR/cockpit-cronplus/manifest.json"
if [ -f "$MANIFEST_PATH" ]; then
    # Use delimiter # instead of / to avoid path conflicts
    sed -i "s#\"plugin_version\": \"[^\"]*\"#\"plugin_version\": \"${VERSION}\"#" "$MANIFEST_PATH"
fi

echo "==> Building cronplus v${VERSION} for ${ARCH}"

# ── 3. 交叉编译 Go 二进制文件 ──
echo "[0/4] Compiling Go binaries for ${ARCH}..."
cd "$SRC"
CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} go build -ldflags "-s -w -X main.version=${VERSION}" -o bin/cronplusd ./cmd/cronplusd
CGO_ENABLED=0 GOOS=linux GOARCH=${ARCH} go build -ldflags "-s -w -X main.version=${VERSION}" -o bin/cronplus ./cmd/cronplus
cd "$DIR"

DIST_DIR="$DIR/dist/${ARCH}"
rm -rf "$DIST_DIR"
mkdir -p "$DIST_DIR"

# ── 4. 后端 deb 打包 ──
echo "[1/4] Packaging cronplus backend (${ARCH})..."
PKG="$DIR/dist/cronplus_${VERSION}_${ARCH}"
rm -rf "$PKG"
mkdir -p "$PKG"/{DEBIAN,usr/bin,opt/cronplus/logs,lib/systemd/system}

cp "$SRC/bin/cronplusd" "$PKG/usr/bin/"
cp "$SRC/bin/cronplus"  "$PKG/usr/bin/"
chmod 755 "$PKG/usr/bin/cronplusd" "$PKG/usr/bin/cronplus"
cp "$SRC/cronplus.service" "$PKG/lib/systemd/system/"

# Set secure permissions for data directory
chmod 700 "$PKG/opt/cronplus"
chmod 700 "$PKG/opt/cronplus/logs"

# 注入维护者脚本
cat > "$PKG/DEBIAN/postinst" <<'EOF'
#!/bin/bash
set -e
mkdir -p /opt/cronplus/logs
# Restrict permissions: only root can read config/task data
chmod 700 /opt/cronplus
chmod 700 /opt/cronplus/logs
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable cronplus.service
    systemctl start cronplus.service || true
fi
EOF

cat > "$PKG/DEBIAN/prerm" <<'EOF'
#!/bin/bash
set -e
if [ -d /run/systemd/system ]; then
    systemctl stop cronplus.service || true
    systemctl disable cronplus.service || true
fi
EOF

cat > "$PKG/DEBIAN/postrm" <<'EOF'
#!/bin/bash
set -e
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi
if [ "$1" = "purge" ]; then
    rm -rf /opt/cronplus
fi
EOF

chmod 755 "$PKG/DEBIAN"/{postinst,prerm,postrm}

cat > "$PKG/DEBIAN/control" <<EOF
Package: cronplus
Version: ${VERSION}
Architecture: ${ARCH}
Maintainer: cronplus <cronplus@localhost>
Description: Advanced Cron Task Manager
Depends: systemd
EOF

dpkg-deb --build "$PKG" "$DIST_DIR/cronplus_${VERSION}_${ARCH}.deb"

# ── 5. Cockpit WebUI deb 打包 ──
echo "[2/4] Packaging cockpit-cronplus WebUI (${ARCH})..."
WPKG="$DIR/dist/cockpit-cronplus_${VERSION}_${ARCH}"
rm -rf "$WPKG"
mkdir -p "$WPKG"/{DEBIAN,usr/share/cockpit/cronplus}

cp -r "$DIR/cockpit-cronplus/"* "$WPKG/usr/share/cockpit/cronplus/"

cat > "$WPKG/DEBIAN/control" <<EOF
Package: cockpit-cronplus
Version: ${VERSION}
Architecture: ${ARCH}
Maintainer: cronplus <cronplus@localhost>
Description: Advanced Cron Task Manager - Cockpit UI
Depends: cronplus (= ${VERSION}), cockpit
EOF

dpkg-deb --build "$WPKG" "$DIST_DIR/cockpit-cronplus_${VERSION}_${ARCH}.deb"

# ── 6. 生成校验和 ──
echo "[3/4] Generating checksums..."
cd "$DIST_DIR"
sha256sum *.deb > SHA256SUMS

# ── 7. 清理 ──
rm -rf "$SRC/bin" "$PKG" "$WPKG"
echo "Done. Output: $DIST_DIR"
