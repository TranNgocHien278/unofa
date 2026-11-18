#!/bin/sh
# Run by the nginx image entrypoint (/docker-entrypoint.d) before nginx starts.
# UNOFA_ADMIN=false drops the admin.* server block, so the admin site is unreachable from outside.
set -eu

if [ "${UNOFA_ADMIN:-true}" != "true" ]; then
    rm -f /etc/nginx/conf.d/admin.conf
    echo "admin-toggle: UNOFA_ADMIN=${UNOFA_ADMIN}, admin site disabled"
fi
