#!/bin/sh
# deb passes "remove"/"upgrade", rpm passes 0 (uninstall) / 1 (upgrade):
# only stop the service on a real removal
set -e

case "$1" in
    remove|0)
        if command -v systemctl >/dev/null 2>&1; then
            systemctl stop concrnt.service >/dev/null 2>&1 || true
            systemctl disable concrnt.service >/dev/null 2>&1 || true
        fi
        ;;
esac
