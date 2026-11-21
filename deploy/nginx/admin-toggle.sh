#!/bin/sh
# Run by the nginx image entrypoint (/docker-entrypoint.d) before nginx starts.
# Links each site config mounted read-only at /etc/nginx/unofa (deploy/nginx on the host) into conf.d,
# so editing a mounted file only needs `nginx -s reload`.
# UNOFA_ADMIN=false drops the admin.* server block, so the admin site is unreachable from outside.
set -eu

rm -f /etc/nginx/conf.d/*.conf
for conf in /etc/nginx/unofa/*.conf; do
    [ -e "$conf" ] || { echo "admin-toggle: no *.conf in /etc/nginx/unofa, mount deploy/nginx there" >&2; exit 1; }
    if [ "${conf##*/}" = admin.conf ] && [ "${UNOFA_ADMIN:-true}" != "true" ]; then
        echo "admin-toggle: UNOFA_ADMIN=${UNOFA_ADMIN}, admin site disabled"
        continue
    fi
    ln -s "$conf" /etc/nginx/conf.d/
done
