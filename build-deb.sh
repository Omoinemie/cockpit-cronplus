#!/bin/bash
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$DIR/cronplus"

# ── 读取版本信息 ──
VERSION=$(cat "$DIR/VERSION" 2>/dev/null | tr -d '[:space:]')
if [ -z "$VERSION" ]; then
    echo "ERROR: VERSION file not found or empty"
    exit 1
fi

echo "==> Building cronplus v${VERSION}"

# ── 编译 Go 二进制文件 ──
echo "[0/4] Compiling Go binaries..."
cd "$SRC"
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o bin/cronplusd ./cmd/cronplusd
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o bin/cronplus ./cmd/cronplus
cd "$DIR"
echo "  -> cronplusd v${VERSION}, cronplus v${VERSION}"

rm -rf "$DIR/dist"
mkdir -p "$DIR/dist"

# ── 后端 deb 打包 ──
echo "[1/4] Packaging cronplus backend..."
PKG="$DIR/dist/cronplus_${VERSION}_amd64"
mkdir -p "$PKG"/{DEBIAN,usr/bin,opt/cronplus/logs,lib/systemd/system}

cp "$SRC/bin/cronplusd" "$PKG/usr/bin/"
cp "$SRC/bin/cronplus"  "$PKG/usr/bin/"
chmod 755 "$PKG/usr/bin/cronplusd" "$PKG/usr/bin/cronplus"

cp "$SRC/cronplus.service" "$PKG/lib/systemd/system/"

# ── 注入维护者脚本 (合并后的部分) ──

# 1. postinst [cite: 1, 2, 3]
cat > "$PKG/DEBIAN/postinst" <<'EOF'
#!/bin/bash
set -e
# 创建数据目录
mkdir -p /opt/cronplus/logs
chmod 755 /opt/cronplus
chmod 755 /opt/cronplus/logs
# 重载 systemd 并启用服务
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
    systemctl enable cronplus.service
    systemctl start cronplus.service || true
fi
echo "cronplus installed and started."
EOF

# 2. prerm [cite: 7, 8]
cat > "$PKG/DEBIAN/prerm" <<'EOF'
#!/bin/bash
set -e
# 停止并禁用服务
if [ -d /run/systemd/system ]; then
    systemctl stop cronplus.service || true
    systemctl disable cronplus.service || true
fi
EOF

# 3. postrm [cite: 4, 5, 6]
cat > "$PKG/DEBIAN/postrm" <<'EOF'
#!/bin/bash
set -e
# 卸载后重载 systemd
if [ -d /run/systemd/system ]; then
    systemctl daemon-reload
fi
# 如果是彻底删除(purge)，则删除数据目录
if [ "$1" = "purge" ]; then
    rm -rf /opt/cronplus
fi
EOF

# 设置脚本执行权限
chmod 755 "$PKG/DEBIAN"/{postinst,prerm,postrm}

# 生成 control 文件
cat > "$PKG/DEBIAN/control" <<EOF
Package: cronplus
Version: ${VERSION}
Architecture: amd64
Maintainer: cronplus <cronplus@localhost>
Description: Advanced Cron Task Manager - daemon and CLI
 Cronplus is an advanced cron task manager with a web-based UI,
 real-time log streaming, and process isolation.
Depends: systemd
EOF

dpkg-deb --build "$PKG" "$DIR/dist/cronplus_${VERSION}_amd64.deb"
echo "  -> cronplus_${VERSION}_amd64.deb"

# ── Cockpit WebUI deb 打包 ──
echo "[2/4] Packaging cockpit-cronplus WebUI..."
WPKG="$DIR/dist/cockpit-cronplus_${VERSION}_amd64"
mkdir -p "$WPKG"/{DEBIAN,usr/share/cockpit/cronplus}

cp -r "$DIR/cockpit-cronplus/"* "$WPKG/usr/share/cockpit/cronplus/"

sed -i "s/\"plugin_version\": \"[^\"]*\"/\"plugin_version\": \"${VERSION}\"/" \
    "$WPKG/usr/share/cockpit/cronplus/manifest.json"

cat > "$WPKG/DEBIAN/control" <<EOF
Package: cockpit-cronplus
Version: ${VERSION}
Architecture: amd64
Maintainer: cronplus <cronplus@localhost>
Description: Advanced Cron Task Manager - Cockpit WebUI Plugin
 Cronplus web interface for Cockpit. Manage cron tasks, view logs,
 and configure schedules from your browser.
Depends: cronplus (= ${VERSION}), cockpit
EOF

dpkg-deb --build "$WPKG" "$DIR/dist/cockpit-cronplus_${VERSION}_amd64.deb"
echo "  -> cockpit-cronplus_${VERSION}_amd64.deb"

# ── 生成校验和 ──
echo "[3/4] Generating checksums..."
cd "$DIR/dist"
sha256sum *.deb > SHA256SUMS
cat SHA256SUMS

# ── 清理 ──
echo "[4/4] Cleaning up..."
rm -rf "$SRC/bin"

echo ""
echo "Done:"
ls -lh "$DIR/dist/"*.deb