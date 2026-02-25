#!/usr/bin/env bash
# Setup script for PoolForge test EC2 instance
set -euo pipefail

apt-get update -qq
apt-get install -y -qq mdadm lvm2 gdisk e2fsprogs

# Copy the poolforge binary (uploaded by test_runner.sh)
chmod +x /tmp/poolforge
mv /tmp/poolforge /usr/local/bin/poolforge

echo "PoolForge test environment ready"
