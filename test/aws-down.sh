#!/bin/bash
set -euo pipefail

# PoolForge Test Environment — Tear Down
REGION="${PF_REGION:-us-east-2}"
ENV_FILE="/tmp/pf-test-env.json"

[[ ! -f "$ENV_FILE" ]] && { echo "No environment file at $ENV_FILE"; exit 1; }

echo "=== Tearing Down Test Environment ==="

# Terminate instances
INSTANCE_IDS=$(jq -r '.nodes[].instance_id' "$ENV_FILE")
if [[ -n "$INSTANCE_IDS" ]]; then
  echo "Terminating instances: $INSTANCE_IDS"
  aws ec2 terminate-instances --region "$REGION" --instance-ids $INSTANCE_IDS > /dev/null
  aws ec2 wait instance-terminated --region "$REGION" --instance-ids $INSTANCE_IDS
  echo "✓ Instances terminated"
fi

# Delete volumes
VOLUME_IDS=$(jq -r '.nodes[].volumes[]' "$ENV_FILE")
for vol in $VOLUME_IDS; do
  aws ec2 delete-volume --region "$REGION" --volume-id "$vol" 2>/dev/null && echo "Deleted $vol" || echo "Skip $vol (already deleted)"
done
echo "✓ Volumes deleted"

rm -f "$ENV_FILE"
echo "✓ Environment cleaned up"
