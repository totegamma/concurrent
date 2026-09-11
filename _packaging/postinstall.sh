#!/bin/sh
# runs after install and after upgrade (deb: configure, rpm: 1 or 2)
set -e

if ! getent group concrnt >/dev/null; then
    groupadd --system concrnt
fi
if ! getent passwd concrnt >/dev/null; then
    useradd --system --gid concrnt --no-create-home --home-dir /nonexistent \
        --shell /usr/sbin/nologin --comment "concrnt server" concrnt
fi

# config holds the server private key: readable by the service user only
chown -R root:concrnt /etc/concrnt
chmod 750 /etc/concrnt/config
find /etc/concrnt/config -type f -exec chmod 640 {} +

if command -v systemctl >/dev/null 2>&1; then
    systemctl daemon-reload >/dev/null 2>&1 || true
    # pick up the new binary if the service is already running (upgrade)
    systemctl try-restart concrnt.service >/dev/null 2>&1 || true
fi
