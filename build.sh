#!/bin/zsh
set -eu
ATLAS_PROJECT="${0:A:h}"
ATLAS_BUILD_DIR="${ATLAS_BUILD_DIR:-$ATLAS_PROJECT/build}"
ATLAS_BACKEND="${ATLAS_BACKEND:-go}"
ATLAS_REGISTER_APP="${ATLAS_REGISTER_APP:-0}"
[[ "$ATLAS_BACKEND" = go ]] || { print -u2 "The Python backend is archived under old/"; exit 1; }
[[ "$ATLAS_REGISTER_APP" = 0 ]] || { print -u2 "Builds do not register or install apps; use the service setup instructions"; exit 1; }
[[ "$ATLAS_BUILD_DIR" = /* ]] || { print -u2 "ATLAS_BUILD_DIR must be absolute"; exit 1; }
ATLAS_BUILD_DIR="${ATLAS_BUILD_DIR:A}"
if [[ -z "${ATLAS_GO_SERVICE:-}" ]]; then
  mkdir -p "$ATLAS_PROJECT/service/.work"
  (cd "$ATLAS_PROJECT/service" && CGO_ENABLED=0 GOPROXY=off GOTOOLCHAIN=local go build -trimpath -o .work/atlas-service ./cmd/atlas-service)
  ATLAS_GO_SERVICE="$ATLAS_PROJECT/service/.work/atlas-service"
fi
[[ "$ATLAS_GO_SERVICE" = /* && -f "$ATLAS_GO_SERVICE" ]] || { print -u2 "ATLAS_GO_SERVICE must name an absolute Go executable"; exit 1; }
/usr/bin/python3 "$ATLAS_PROJECT/service/tools/notices.py" --check
(cd "$ATLAS_PROJECT/service" && CGO_ENABLED=0 GOPROXY=off GOTOOLCHAIN=local go run ./tools/embedweb --check --source "$ATLAS_PROJECT/web" --output "$ATLAS_PROJECT/service/internal/assets/generated.go")
ATLAS_BUNDLE_ID="local.atlas.go-preview"
ATLAS_REQUIRE_SERVICE="<key>AtlasRequireService</key><true/>"
ATLAS_APP="$ATLAS_BUILD_DIR/Atlas.app"
# Stage a new bundle, avoiding stale resources from previous backend builds.
ATLAS_STAGE="$ATLAS_BUILD_DIR/.Atlas-staging-$$.app"
trap 'rm -rf "$ATLAS_STAGE"' EXIT
ATLAS_FINAL_APP="$ATLAS_APP"
ATLAS_APP="$ATLAS_STAGE"
# Reuse a certificate-backed identity across rebuilds; never silently sign ad hoc.
ATLAS_SIGNING_IDENTITY="${ATLAS_SIGNING_IDENTITY:-Atlas Local Development}"
[[ "$ATLAS_SIGNING_IDENTITY" != - ]] || { print -u2 "Atlas requires a certificate-backed signing identity"; exit 1; }
mkdir -p "$ATLAS_APP/Contents/MacOS" "$ATLAS_APP/Contents/Resources" "$ATLAS_BUILD_DIR/module-cache"
/usr/bin/swiftc -module-cache-path "$ATLAS_BUILD_DIR/module-cache" "$ATLAS_PROJECT/app/Atlas.swift" -o "$ATLAS_APP/Contents/MacOS/Atlas" -framework Cocoa -framework WebKit
/usr/bin/swiftc -module-cache-path "$ATLAS_BUILD_DIR/module-cache" "$ATLAS_PROJECT/scripts/create-icon.swift" -o "$ATLAS_BUILD_DIR/create-icon" -framework Cocoa
"$ATLAS_BUILD_DIR/create-icon" "$ATLAS_BUILD_DIR/Atlas.iconset"
/usr/bin/iconutil -c icns "$ATLAS_BUILD_DIR/Atlas.iconset" -o "$ATLAS_APP/Contents/Resources/Atlas.icns"
cp "$ATLAS_PROJECT/service/THIRD_PARTY_NOTICES.txt" "$ATLAS_APP/Contents/Resources/ThirdPartyNotices.txt"
mkdir -p "$ATLAS_APP/Contents/Resources/bin"
cp "$ATLAS_GO_SERVICE" "$ATLAS_APP/Contents/Resources/bin/atlas-service"
/usr/bin/codesign --force --sign "$ATLAS_SIGNING_IDENTITY" "$ATLAS_APP/Contents/Resources/bin/atlas-service"
cat > "$ATLAS_APP/Contents/Info.plist" <<PLIST
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>Atlas</string><key>CFBundleIdentifier</key><string>$ATLAS_BUNDLE_ID</string><key>CFBundleName</key><string>Atlas</string><key>CFBundleDisplayName</key><string>Atlas</string><key>CFBundleShortVersionString</key><string>0.1</string><key>CFBundleVersion</key><string>3</string><key>CFBundleIconFile</key><string>Atlas.icns</string><key>CFBundlePackageType</key><string>APPL</string>$ATLAS_REQUIRE_SERVICE<key>NSHighResolutionCapable</key><true/><key>NSDocumentsFolderUsageDescription</key><string>Atlas reads and updates your local knowledge library in Documents, including while its window is closed.</string><key>NSAppTransportSecurity</key><dict><key>NSAllowsLocalNetworking</key><true/></dict></dict></plist>
PLIST
/usr/bin/touch "$ATLAS_APP/Contents/Info.plist" "$ATLAS_APP"
# A separate background app identity keeps launching the Atlas window independent.
ATLAS_SERVICE_APP="$ATLAS_APP/Contents/Library/LoginItems/AtlasService.app"
mkdir -p "$ATLAS_SERVICE_APP/Contents/MacOS"
cp "$ATLAS_APP/Contents/MacOS/Atlas" "$ATLAS_SERVICE_APP/Contents/MacOS/AtlasService"
cat > "$ATLAS_SERVICE_APP/Contents/Info.plist" <<SERVICE_PLIST
<?xml version="1.0" encoding="UTF-8"?><!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd"><plist version="1.0"><dict><key>CFBundleExecutable</key><string>AtlasService</string><key>CFBundleIdentifier</key><string>$ATLAS_BUNDLE_ID.service</string><key>CFBundleName</key><string>Atlas Service</string><key>CFBundleDisplayName</key><string>Atlas Service</string><key>CFBundlePackageType</key><string>APPL</string><key>CFBundleVersion</key><string>1</string><key>LSUIElement</key><true/><key>NSDocumentsFolderUsageDescription</key><string>Atlas Service reads and updates your local knowledge library in Documents while the Atlas window is closed.</string></dict></plist>
SERVICE_PLIST
/usr/bin/codesign --force --sign "$ATLAS_SIGNING_IDENTITY" "$ATLAS_SERVICE_APP"
/usr/bin/codesign --force --sign "$ATLAS_SIGNING_IDENTITY" "$ATLAS_APP"
/usr/bin/codesign --verify --deep --strict "$ATLAS_APP"
# Publish only a fully verified bundle; registration remains an explicit option.
if [[ -e "$ATLAS_FINAL_APP" ]]; then
  ATLAS_PREVIOUS="$ATLAS_BUILD_DIR/.Atlas-previous-$$.app"
  mv "$ATLAS_FINAL_APP" "$ATLAS_PREVIOUS"
  if ! mv "$ATLAS_APP" "$ATLAS_FINAL_APP"; then mv "$ATLAS_PREVIOUS" "$ATLAS_FINAL_APP"; exit 1; fi
  rm -rf "$ATLAS_PREVIOUS"
else
  mv "$ATLAS_APP" "$ATLAS_FINAL_APP"
fi
ATLAS_APP="$ATLAS_FINAL_APP"
ATLAS_SERVICE_APP="$ATLAS_APP/Contents/Library/LoginItems/AtlasService.app"
print "Built $ATLAS_APP ($ATLAS_BACKEND; registration=$ATLAS_REGISTER_APP)"
