#!/bin/sh
set -eu

# Host private files stay mode 0600. Copy only this container's BFF key to a
# private runtime directory before dropping to the existing node user.
: "${PLATFORM_AUTH_SIGNING_KEY_FILE:?BFF signing key file is required}"
mkdir -p /run/bff-keys
chmod 0700 /run/bff-keys
cp "$PLATFORM_AUTH_SIGNING_KEY_FILE" /run/bff-keys/private.pem
chown -R node:node /run/bff-keys
chmod 0400 /run/bff-keys/private.pem
export PLATFORM_AUTH_SIGNING_KEY_FILE=/run/bff-keys/private.pem
exec su-exec node:node "$@"
