#!/bin/bash
set -euo pipefail

# PoolForge Test Environment — Quick SSH
SSH_KEY="${PF_SSH_KEY:-$HOME/.ssh/poolforge-test}"
ENV_FILE="/tmp/pf-test-env.json"
NODE="${1:-0}"

[[ ! -f "$ENV_FILE" ]] && { echo "No environment file at $ENV_FILE"; exit 1; }

IP=$(jq -r ".nodes[$NODE].public_ip" "$ENV_FILE")
[[ "$IP" == "null" ]] && { echo "Node $NODE not found"; exit 1; }

exec ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" "ubuntu@$IP"
