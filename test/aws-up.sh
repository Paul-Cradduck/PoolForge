#!/bin/bash
set -euo pipefail

# PoolForge Test Environment — Provision EC2 + EBS
# Usage: ./test/aws-up.sh --disks 4x10,4x5,4x3 [--nodes 2]

REGION="${PF_REGION:-us-east-2}"
SSH_KEY="${PF_SSH_KEY:-$HOME/.ssh/poolforge-test}"
INSTANCE_TYPE="${PF_INSTANCE_TYPE:-t3.medium}"
SG="${PF_SG:-sg-0024c03c18861945b}"
SUBNET="${PF_SUBNET:-subnet-02f43ab7b6fb088f4}"
KEY_NAME="${PF_KEY_NAME:-poolforge-test}"
ENV_FILE="/tmp/pf-test-env.json"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

NODES=1
DISK_SPEC=""

while [[ $# -gt 0 ]]; do
  case $1 in
    --disks) DISK_SPEC="$2"; shift 2 ;;
    --nodes) NODES="$2"; shift 2 ;;
    *) echo "Unknown: $1"; exit 1 ;;
  esac
done

[[ -z "$DISK_SPEC" ]] && { echo "Usage: $0 --disks 4x10,4x5,4x3 [--nodes N]"; exit 1; }

# Find latest Ubuntu 24.04 AMI
AMI="${PF_AMI:-$(aws ec2 describe-images --region "$REGION" \
  --filters "Name=name,Values=ubuntu/images/hvm-ssd-gp3/ubuntu-noble-24.04-amd64-server-*" \
  --owners 099720109477 --query 'sort_by(Images, &CreationDate)[-1].ImageId' --output text)}"

echo "=== PoolForge Test Environment ==="
echo "Region: $REGION  AMI: $AMI  Type: $INSTANCE_TYPE"
echo "Disks: $DISK_SPEC  Nodes: $NODES"

# Build binary
echo "Building poolforge binary..."
cd "$PROJECT_DIR"
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
GOOS=linux GOARCH=amd64 go build -o "$PROJECT_DIR/poolforge" ./cmd/poolforge
echo "✓ Binary built"

# Parse disk spec: "4x10,4x5,4x3" → array of sizes
DISK_SIZES=()
IFS=',' read -ra SPECS <<< "$DISK_SPEC"
for spec in "${SPECS[@]}"; do
  count="${spec%%x*}"
  size="${spec##*x}"
  for ((i=0; i<count; i++)); do
    DISK_SIZES+=("$size")
  done
done
echo "Total disks per node: ${#DISK_SIZES[@]}"

# Launch nodes
NODES_JSON="[]"
for ((n=0; n<NODES; n++)); do
  echo "--- Node $n ---"

  # Launch instance
  INSTANCE_ID=$(aws ec2 run-instances --region "$REGION" \
    --image-id "$AMI" --instance-type "$INSTANCE_TYPE" \
    --key-name "$KEY_NAME" --security-group-ids "$SG" --subnet-id "$SUBNET" \
    --associate-public-ip-address \
    --tag-specifications "ResourceType=instance,Tags=[{Key=Name,Value=poolforge-test-node${n}}]" \
    --query 'Instances[0].InstanceId' --output text)
  echo "Instance: $INSTANCE_ID"

  # Wait for running
  aws ec2 wait instance-running --region "$REGION" --instance-ids "$INSTANCE_ID"
  PUBLIC_IP=$(aws ec2 describe-instances --region "$REGION" --instance-ids "$INSTANCE_ID" \
    --query 'Reservations[0].Instances[0].PublicIpAddress' --output text)
  echo "IP: $PUBLIC_IP"

  # Get AZ for volumes
  AZ=$(aws ec2 describe-instances --region "$REGION" --instance-ids "$INSTANCE_ID" \
    --query 'Reservations[0].Instances[0].Placement.AvailabilityZone' --output text)

  # Create and attach EBS volumes
  VOL_IDS=()
  for ((d=0; d<${#DISK_SIZES[@]}; d++)); do
    VOL_ID=$(aws ec2 create-volume --region "$REGION" \
      --availability-zone "$AZ" --size "${DISK_SIZES[$d]}" --volume-type gp3 \
      --tag-specifications "ResourceType=volume,Tags=[{Key=Name,Value=poolforge-test-node${n}-disk${d}}]" \
      --query 'VolumeId' --output text)
    VOL_IDS+=("$VOL_ID")
  done
  echo "Volumes: ${VOL_IDS[*]}"

  # Wait for volumes and attach
  for ((d=0; d<${#VOL_IDS[@]}; d++)); do
    aws ec2 wait volume-available --region "$REGION" --volume-ids "${VOL_IDS[$d]}"
    # Device names: /dev/sdf, /dev/sdg, ...
    DEV_LETTER=$(printf "\\x$(printf '%02x' $((102 + d)))")
    aws ec2 attach-volume --region "$REGION" \
      --volume-id "${VOL_IDS[$d]}" --instance-id "$INSTANCE_ID" \
      --device "/dev/sd${DEV_LETTER}" > /dev/null
  done
  echo "✓ Volumes attached"

  # Wait for SSH
  echo -n "Waiting for SSH..."
  for i in $(seq 1 60); do
    if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=3 -i "$SSH_KEY" "ubuntu@$PUBLIC_IP" true 2>/dev/null; then
      break
    fi
    echo -n "."
    sleep 3
  done
  echo " ready"

  # Deploy binary and install deps
  scp -o StrictHostKeyChecking=no -i "$SSH_KEY" "$PROJECT_DIR/poolforge" "ubuntu@$PUBLIC_IP:/tmp/poolforge"
  ssh -o StrictHostKeyChecking=no -o ServerAliveInterval=10 -i "$SSH_KEY" "ubuntu@$PUBLIC_IP" "
    # Wait for cloud-init apt to finish
    while sudo fuser /var/lib/apt/lists/lock >/dev/null 2>&1 || sudo fuser /var/lib/dpkg/lock-frontend >/dev/null 2>&1; do sleep 2; done
    sudo cloud-init status --wait >/dev/null 2>&1 || true
    
    # Install deps
    sudo apt-get update -qq
    sudo DEBIAN_FRONTEND=noninteractive apt-get install -y -qq mdadm lvm2 smartmontools samba nfs-kernel-server > /dev/null 2>&1
    
    # Deploy binary
    sudo mv /tmp/poolforge /usr/local/bin/poolforge && sudo chmod +x /usr/local/bin/poolforge
    sudo mkdir -p /var/lib/poolforge
    
    # Create systemd service
    sudo tee /etc/systemd/system/poolforge.service > /dev/null <<'SVC'
[Unit]
Description=PoolForge Storage Manager
After=network.target mdadm.service lvm2-activation.service
[Service]
Type=simple
EnvironmentFile=-/etc/poolforge.conf
ExecStart=/bin/bash -c '/usr/local/bin/poolforge serve --addr \${POOLFORGE_ADDR:-0.0.0.0:8080} \${POOLFORGE_USER:+--user \$POOLFORGE_USER} \${POOLFORGE_PASS:+--pass \$POOLFORGE_PASS}'
Restart=on-failure
RestartSec=5
[Install]
WantedBy=multi-user.target
SVC
    
    # Configure and start
    sudo tee /etc/poolforge.conf > /dev/null <<'CONF'
POOLFORGE_USER=admin
POOLFORGE_PASS=admin
POOLFORGE_ADDR=0.0.0.0:8080
CONF
    sudo systemctl daemon-reload
    sudo systemctl enable poolforge
    sudo systemctl restart poolforge
    sleep 2
    sudo systemctl status poolforge --no-pager -l 2>&1 | head -5
  "

  # Build node JSON
  VOL_JSON=$(printf '%s\n' "${VOL_IDS[@]}" | jq -R . | jq -s .)
  NODE_JSON=$(jq -n \
    --arg id "$INSTANCE_ID" --arg ip "$PUBLIC_IP" \
    --argjson vols "$VOL_JSON" \
    '{instance_id: $id, public_ip: $ip, volumes: $vols}')
  NODES_JSON=$(echo "$NODES_JSON" | jq ". + [$NODE_JSON]")
done

# Write env file
jq -n --arg region "$REGION" --argjson nodes "$NODES_JSON" \
  '{region: $region, nodes: $nodes}' > "$ENV_FILE"

echo ""
echo "=== Environment Ready ==="
cat "$ENV_FILE" | jq .
echo ""
echo "Portal: http://$(echo "$NODES_JSON" | jq -r '.[0].public_ip'):8080  (admin/admin)"
echo "SSH:    ./test/aws-ssh.sh"
echo "Tear down: ./test/aws-down.sh"
