#!/usr/bin/env bash
# Rebuilds rsrc_windows_amd64.syso (icon, manifest and version info) from res/.
# Needs the MinGW resource compiler: sudo apt-get install binutils-mingw-w64-x86-64
# Only required when you change something in res/. The .syso is committed.
set -euo pipefail
cd "$(dirname "$0")/../res"
x86_64-w64-mingw32-windres --preprocessor=cpp -O coff -i app.rc -o ../rsrc_windows_amd64.syso
echo "Wrote rsrc_windows_amd64.syso"
