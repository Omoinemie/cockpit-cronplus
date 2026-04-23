#!/bin/bash
set -e

DIR="$(cd "$(dirname "$0")" && pwd)"
SRC="$DIR/cronplus"

# ── 处理版本输入与同步 ──
# 如果脚本执行时带了参数 (例如: ./build-deb.sh 1.0.1)
if [ -n "$1" ]; then
    echo "$1" > "$DIR/VERSION"
    echo "==> Version file updated to: $1"
fi

# 读取最终确定的版本号
VERSION=$(cat "$DIR/VERSION" 2>/dev/null | tr -d '[:space:]')
if [ -z "$VERSION" ]; then
    echo "ERROR: VERSION file not found or empty"
    exit 1
fi

# 同步更新源码中的 manifest.json (确保源码版本与构建版本一致)
MANIFEST_PATH="$DIR/cockpit-cronplus/manifest.json"
if [ -f "$MANIFEST_PATH" ]; then
    sed -i "s/\"plugin_version\": \"[^\"]*\"/\"plugin_version\": \"${VERSION}\"/" "$MANIFEST_PATH"
    echo "==> Source manifest.json updated to: ${VERSION}"
fi

echo "==> Building cronplus v${VERSION}"

# ── 编译 Go 二进制文件 ──
echo "[0/4] Compiling Go binaries..."
cd "$SRC"
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o bin/cronplusd ./cmd/cronplusd
CGO_ENABLED=0 go build -ldflags "-s -w -X main.version=${VERSION}" -o bin/cronplus ./cmd/cronplus
cd "$DIR"

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

# 注入维护者脚本
cat > "$PKG/DEBIAN/postinst" <<'EOF'
#!/bin/bash
set -e
mkdir -p /opt/cronplus/logs
chmod 755 /opt/cronplus /opt/cronplus/logs
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
Architecture: amd64
Maintainer: cronplus <cronplus@localhost>
Description: Advanced Cron Task Manager
Depends: systemd
EOF

dpkg-deb --build "$PKG" "$DIR/dist/cronplus_${VERSION}_amd64.deb"

# ── Cockpit WebUI deb 打包 ──
echo "[2/4] Packaging cockpit-cronplus WebUI..."
WPKG="$DIR/dist/cockpit-cronplus_${VERSION}_amd64"
mkdir -p "$WPKG"/{DEBIAN,usr/share/cockpit/cronplus}

# 复制已同步过版本号的源码
cp -r "$DIR/cockpit-cronplus/"* "$WPKG/usr/share/cockpit/cronplus/"

cat > "$WPKG/DEBIAN/control" <<EOF
Package: cockpit-cronplus
Version: ${VERSION}
Architecture: amd64
Maintainer: cronplus <cronplus@localhost>
Description: Advanced Cron Task Manager - Cockpit UI
Depends: cronplus (= ${VERSION}), cockpit
EOF

dpkg-deb --build "$WPKG" "$DIR/dist/cockpit-cronplus_${VERSION}_amd64.deb"

# ── 生成校验和 ──
echo "[3/4] Generating checksums..."
cd "$DIR/dist"
sha256sum *.deb > SHA256SUMS

# ── 清理 ──
echo "[4/4] Cleaning up..."
rm -rf "$SRC/bin"

echo "Done. Output in $DIR/dist/"
