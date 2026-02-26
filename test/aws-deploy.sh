#!/bin/bash
set -euo pipefail

# PoolForge Test Environment — Rebuild and Redeploy
SSH_KEY="${PF_SSH_KEY:-$HOME/.ssh/poolforge-test}"
ENV_FILE="/tmp/pf-test-env.json"
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_DIR="$(dirname "$SCRIPT_DIR")"

[[ ! -f "$ENV_FILE" ]] && { echo "No environment file at $ENV_FILE"; exit 1; }

echo "Building poolforge binary..."
cd "$PROJECT_DIR"
export PATH=$PATH:/usr/local/go/bin:$HOME/go/bin
GOOS=linux GOARCH=amd64 go build -o "$PROJECT_DIR/poolforge" ./cmd/poolforge
echo "✓ Binary built"

NODES=$(jq -r '.nodes | length' "$ENV_FILE")
for ((n=0; n<NODES; n++)); do
  IP=$(jq -r ".nodes[$n].public_ip" "$ENV_FILE")
  echo "Deploying to node $n ($IP)..."
  scp -o StrictHostKeyChecking=no -i "$SSH_KEY" "$PROJECT_DIR/poolforge" "ubuntu@$IP:/tmp/poolforge"
  ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "ubuntu@$IP" "
    sudo systemctl stop poolforge 2>/dev/null || true
    sudo mv /tmp/poolforge /usr/local/bin/poolforge && sudo chmod +x /usr/local/bin/poolforge
    sudo systemctl start poolforge
  "
  echo "✓ Node $n deployed"
done
echo "Done"
