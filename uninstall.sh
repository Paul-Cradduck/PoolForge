#!/bin/bash
set -e

# PoolForge Uninstaller
# Usage: sudo bash uninstall.sh

RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
NC='\033[0m'

echo "╔══════════════════════════════════╗"
echo "║      PoolForge Uninstaller       ║"
echo "╚══════════════════════════════════╝"

if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}Error: must run as root${NC}"
  exit 1
fi

# Check for active pools
if [ -f /var/lib/poolforge/metadata.json ]; then
  POOLS=$(python3 -c "import json; d=json.load(open('/var/lib/poolforge/metadata.json')); print(len(d.get('pools',{})))" 2>/dev/null || echo "0")
  if [ "$POOLS" != "0" ]; then
    echo -e "${YELLOW}WARNING: $POOLS active pool(s) detected!${NC}"
    echo "Uninstalling will NOT destroy your pools or data."
    echo "Arrays and LVM volumes will remain intact on disk."
    echo "You can re-import them later with: poolforge pool import"
    echo ""
    read -p "Continue uninstall? [y/N] " -n 1 -r
    echo ""
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
      echo "Aborted."
      exit 0
    fi
  fi
fi

# Stop and disable service
if systemctl is-active poolforge >/dev/null 2>&1; then
  echo "Stopping PoolForge service..."
  systemctl stop poolforge
fi
if systemctl is-enabled poolforge >/dev/null 2>&1; then
  systemctl disable poolforge 2>/dev/null
fi
rm -f /etc/systemd/system/poolforge.service
systemctl daemon-reload
echo -e "${GREEN}✓ Service removed${NC}"

# Remove binary
rm -f /usr/local/bin/poolforge
echo -e "${GREEN}✓ Binary removed${NC}"

# Ask about config and data
echo ""
read -p "Remove config file /etc/poolforge.conf? [y/N] " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
  rm -f /etc/poolforge.conf
  echo -e "${GREEN}✓ Config removed${NC}"
fi

read -p "Remove metadata and logs in /var/lib/poolforge/? [y/N] " -n 1 -r
echo ""
if [[ $REPLY =~ ^[Yy]$ ]]; then
  rm -rf /var/lib/poolforge
  echo -e "${GREEN}✓ Data directory removed${NC}"
else
  echo -e "${YELLOW}  Kept /var/lib/poolforge/ (metadata preserved for re-import)${NC}"
fi

echo ""
echo -e "${GREEN}══════════════════════════════════════${NC}"
echo -e "${GREEN}  PoolForge uninstalled.${NC}"
echo -e "${GREEN}══════════════════════════════════════${NC}"
echo ""
echo "Note: mdadm, lvm2, and smartmontools were NOT removed."
echo "Your RAID arrays, LVM volumes, and data are untouched."
echo "To re-install: curl -sSL https://raw.githubusercontent.com/Paul-Cradduck/PoolForge/main/install.sh | sudo bash"
echo ""
