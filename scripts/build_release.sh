#!/usr/bin/env bash
set -e

VERSION=${1:-"v1.0.0"}
OUT_DIR="dist"
rm -rf "${OUT_DIR}"
mkdir -p "${OUT_DIR}"

echo "=========================================="
echo "💧 Building TinyDrop (TD) Release: ${VERSION}"
echo "=========================================="

echo "-> Generating GUI Icons (.icns, .ico, .syso)..."
go run scripts/gen_icons.go

# 1. Windows (amd64 & arm64) - GUI Tray Mode (with embedded TD icon, no black console window)
echo "-> Building Windows amd64..."
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags="-s -w -H windowsgui -X main.Version=${VERSION}" -o "${OUT_DIR}/TinyDrop.exe" .
(cd "${OUT_DIR}" && zip -q "TinyDrop_${VERSION}_windows_amd64.zip" TinyDrop.exe && rm -f TinyDrop.exe)

echo "-> Building Windows arm64..."
CGO_ENABLED=0 GOOS=windows GOARCH=arm64 go build -trimpath -ldflags="-s -w -H windowsgui -X main.Version=${VERSION}" -o "${OUT_DIR}/TinyDrop.exe" .
(cd "${OUT_DIR}" && zip -q "TinyDrop_${VERSION}_windows_arm64.zip" TinyDrop.exe && rm -f TinyDrop.exe)

# 2. Linux (amd64 & arm64)
echo "-> Building Linux amd64..."
CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o "${OUT_DIR}/tinydrop" .
(cd "${OUT_DIR}" && tar -czf "TinyDrop_${VERSION}_linux_amd64.tar.gz" tinydrop && rm -f tinydrop)

echo "-> Building Linux arm64..."
CGO_ENABLED=0 GOOS=linux GOARCH=arm64 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o "${OUT_DIR}/tinydrop" .
(cd "${OUT_DIR}" && tar -czf "TinyDrop_${VERSION}_linux_arm64.tar.gz" tinydrop && rm -f tinydrop)

# 3. macOS (amd64, arm64, Universal Binary & TinyDrop.app)
if [[ "$(uname -s)" == "Darwin" ]]; then
  CLEAN_VER="${VERSION#v}"
  export MACOSX_DEPLOYMENT_TARGET=11.0
  export CGO_CFLAGS="-mmacosx-version-min=11.0"
  export CGO_LDFLAGS="-mmacosx-version-min=11.0"

  echo "-> Building macOS amd64 (Intel)..."
  CGO_ENABLED=1 GOOS=darwin GOARCH=amd64 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o "${OUT_DIR}/TinyDrop_darwin_amd64" .

  echo "-> Building macOS arm64 (Apple Silicon)..."
  CGO_ENABLED=1 GOOS=darwin GOARCH=arm64 go build -trimpath -ldflags="-s -w -X main.Version=${VERSION}" -o "${OUT_DIR}/TinyDrop_darwin_arm64" .

  echo "-> Creating macOS Universal Binary & TinyDrop.app Bundle..."
  lipo -create -output "${OUT_DIR}/TinyDrop_darwin_universal" "${OUT_DIR}/TinyDrop_darwin_amd64" "${OUT_DIR}/TinyDrop_darwin_arm64"
  chmod +x "${OUT_DIR}"/TinyDrop_darwin_*

  APP_CONTENTS="${OUT_DIR}/TinyDrop.app/Contents"
  mkdir -p "${APP_CONTENTS}/MacOS" "${APP_CONTENTS}/Resources"
  cp "${OUT_DIR}/TinyDrop_darwin_universal" "${APP_CONTENTS}/MacOS/TinyDrop"
  if [[ -f "internal/assets/AppIcon.icns" ]]; then
    cp "internal/assets/AppIcon.icns" "${APP_CONTENTS}/Resources/AppIcon.icns"
  fi
  cat > "${APP_CONTENTS}/Info.plist" <<EOF
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
  <key>CFBundleExecutable</key>
  <string>TinyDrop</string>
  <key>CFBundleIdentifier</key>
  <string>com.timtoday.tinydrop</string>
  <key>CFBundleName</key>
  <string>TinyDrop</string>
  <key>CFBundleDisplayName</key>
  <string>TinyDrop (TD)</string>
  <key>CFBundlePackageType</key>
  <string>APPL</string>
  <key>CFBundleIconFile</key>
  <string>AppIcon</string>
  <key>CFBundleVersion</key>
  <string>${CLEAN_VER}</string>
  <key>CFBundleShortVersionString</key>
  <string>${CLEAN_VER}</string>
  <key>LSMinimumSystemVersion</key>
  <string>11.0</string>
  <key>LSUIElement</key>
  <true/>
  <key>NSHighResolutionCapable</key>
  <true/>
</dict>
</plist>
EOF
  codesign --force --deep --sign - "${OUT_DIR}/TinyDrop.app"

  (
    cd "${OUT_DIR}"
    tar -czf "TinyDrop_${VERSION}_darwin_amd64.tar.gz" TinyDrop_darwin_amd64
    tar -czf "TinyDrop_${VERSION}_darwin_arm64.tar.gz" TinyDrop_darwin_arm64
    tar -czf "TinyDrop_${VERSION}_darwin_universal.tar.gz" TinyDrop_darwin_universal
    zip -r -q "TinyDrop_${VERSION}_macOS_App_Universal.zip" TinyDrop.app
    rm -rf TinyDrop.app TinyDrop_darwin_amd64 TinyDrop_darwin_arm64 TinyDrop_darwin_universal
  )
fi

# 4. Generate SHA256 Checksums
(
  cd "${OUT_DIR}"
  shasum -a 256 * > checksums.txt
)

echo "✅ All packages built in ./${OUT_DIR}:"
ls -lh "${OUT_DIR}"
