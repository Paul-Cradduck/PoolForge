#!/bin/bash
set -e

# PoolForge Installer for Ubuntu LTS
# Usage: curl -sSL https://raw.githubusercontent.com/Paul-Cradduck/PoolForge/main/install.sh | sudo bash

RED='\033[0;31m'
GREEN='\033[0;32m'
NC='\033[0m'

echo "╔══════════════════════════════════╗"
echo "║       PoolForge Installer        ║"
echo "╚══════════════════════════════════╝"

# Check root
if [ "$EUID" -ne 0 ]; then
  echo -e "${RED}Error: must run as root${NC}"
  exit 1
fi

# Check Ubuntu
if ! grep -qi ubuntu /etc/os-release 2>/dev/null; then
  echo -e "${RED}Error: Ubuntu LTS required${NC}"
  exit 1
fi

# Check architecture
ARCH=$(uname -m)
if [ "$ARCH" != "x86_64" ]; then
  echo -e "${RED}Error: x86_64 required, got $ARCH${NC}"
  exit 1
fi

echo "Installing dependencies..."
apt-get update -qq
apt-get install -y -qq mdadm lvm2 smartmontools samba nfs-kernel-server curl > /dev/null 2>&1
echo -e "${GREEN}✓ Dependencies installed${NC}"

echo "Downloading PoolForge..."
RELEASE_URL="https://github.com/Paul-Cradduck/PoolForge/releases/latest/download/poolforge-linux-amd64"
if curl -fsSL "$RELEASE_URL" -o /usr/local/bin/poolforge 2>/dev/null; then
  chmod +x /usr/local/bin/poolforge
else
  # Fallback: build from source
  echo "Release binary not found, building from source..."
  apt-get install -y -qq golang-go git > /dev/null 2>&1
  TMPDIR=$(mktemp -d)
  git clone --depth 1 https://github.com/Paul-Cradduck/PoolForge.git "$TMPDIR" 2>/dev/null
  cd "$TMPDIR"
  go build -o /usr/local/bin/poolforge ./cmd/poolforge
  rm -rf "$TMPDIR"
fi
chmod +x /usr/local/bin/poolforge
echo -e "${GREEN}✓ PoolForge binary installed${NC}"

# Create data directory
mkdir -p /var/lib/poolforge
echo -e "${GREEN}✓ Data directory created${NC}"

# Create systemd service
cat > /etc/systemd/system/poolforge.service << 'EOF'
[Unit]
Description=PoolForge Storage Manager
After=network.target mdadm.service lvm2-activation.service
Wants=mdadm.service lvm2-activation.service

[Service]
Type=simple
ExecStart=/usr/local/bin/poolforge serve --addr 0.0.0.0:8080
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF

systemctl daemon-reload
systemctl enable poolforge
echo -e "${GREEN}✓ Systemd service created and enabled${NC}"

# Create config for auth (optional)
if [ ! -f /etc/poolforge.conf ]; then
  cat > /etc/poolforge.conf << 'EOF'
# PoolForge Configuration
# Uncomment and set credentials for web UI authentication
#POOLFORGE_USER=admin
#POOLFORGE_PASS=changeme
#POOLFORGE_ADDR=0.0.0.0:8080
#POOLFORGE_WEBHOOK=
EOF
  echo -e "${GREEN}✓ Config file created at /etc/poolforge.conf${NC}"
fi

# Update service to use config
cat > /etc/systemd/system/poolforge.service << 'EOF'
[Unit]
Description=PoolForge Storage Manager
After=network.target mdadm.service lvm2-activation.service
Wants=mdadm.service lvm2-activation.service

[Service]
Type=simple
EnvironmentFile=-/etc/poolforge.conf
ExecStart=/bin/bash -c '/usr/local/bin/poolforge serve \
  --addr ${POOLFORGE_ADDR:-0.0.0.0:8080} \
  ${POOLFORGE_USER:+--user $POOLFORGE_USER} \
  ${POOLFORGE_PASS:+--pass $POOLFORGE_PASS} \
  ${POOLFORGE_WEBHOOK:+--webhook $POOLFORGE_WEBHOOK}'
Restart=on-failure
RestartSec=5
LimitNOFILE=65536

[Install]
WantedBy=multi-user.target
EOF
systemctl daemon-reload

echo ""
echo -e "${GREEN}══════════════════════════════════════${NC}"
echo -e "${GREEN}  PoolForge installed successfully!${NC}"
echo -e "${GREEN}══════════════════════════════════════${NC}"
echo ""
echo "  Binary:   /usr/local/bin/poolforge"
echo "  Config:   /etc/poolforge.conf"
echo "  Data:     /var/lib/poolforge/"
echo "  Service:  poolforge.service"
echo ""
echo "Quick start:"
echo "  1. Edit /etc/poolforge.conf to set credentials"
echo "  2. sudo systemctl start poolforge"
echo "  3. Open http://$(hostname -I | awk '{print $1}'):8080"
echo ""
echo "CLI usage:"
echo "  poolforge pool create --name mypool --disks /dev/sda,/dev/sdb"
echo "  poolforge pool list"
echo "  poolforge pool import"
echo ""
