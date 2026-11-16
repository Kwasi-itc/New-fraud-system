#!/usr/bin/env bash
set -euo pipefail
if [[ ${EUID} -ne 0 ]]; then
  echo 'Run with sudo on the intended Ubuntu EC2 instance.' >&2
  exit 1
fi
# shellcheck source=/dev/null
. /etc/os-release
if [[ ${ID} != ubuntu || $(dpkg --print-architecture) != amd64 ]]; then
  echo 'This deployment targets Ubuntu amd64; backend images currently build amd64 binaries.' >&2
  exit 1
fi
if command -v docker >/dev/null; then
  docker compose version
  systemctl enable --now docker
  echo 'Docker already installed; preserving its installation and all volumes.'
  exit 0
fi
export DEBIAN_FRONTEND=noninteractive
apt-get update
apt-get install -y ca-certificates curl git
install -m 0755 -d /etc/apt/keyrings
curl --fail --silent --show-error --location https://download.docker.com/linux/ubuntu/gpg -o /etc/apt/keyrings/docker.asc
chmod a+r /etc/apt/keyrings/docker.asc
cat >/etc/apt/sources.list.d/docker.sources <<EOF
Types: deb
URIs: https://download.docker.com/linux/ubuntu
Suites: ${UBUNTU_CODENAME:-$VERSION_CODENAME}
Components: stable
Architectures: amd64
Signed-By: /etc/apt/keyrings/docker.asc
EOF
apt-get update
apt-get install -y docker-ce docker-ce-cli containerd.io docker-buildx-plugin docker-compose-plugin
systemctl enable --now docker
docker compose version
echo 'Docker is ready. Use sudo for deployment, or manage Docker group access explicitly.'
