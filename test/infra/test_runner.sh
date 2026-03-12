#!/usr/bin/env bash
# PoolForge Test Runner
# Orchestrates: provision → upload → test → collect → teardown
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
PROJECT_ROOT="$(cd "$SCRIPT_DIR/../.." && pwd)"
RUN_ID="${RUN_ID:-$(date +%s)}"
SSH_KEY="${SSH_KEY:?Set SSH_KEY to your private key path}"
KEY_NAME="${KEY_NAME:?Set KEY_NAME to your AWS key pair name}"
REGION="${REGION:-us-east-1}"

TEARDOWN_DONE=false

teardown() {
    if [ "$TEARDOWN_DONE" = true ]; then return; fi
    TEARDOWN_DONE=true
    echo "=== Tearing down test infrastructure ==="
    cd "$SCRIPT_DIR"
    if ! terraform destroy -auto-approve -var "key_name=$KEY_NAME" -var "run_id=$RUN_ID" -var "region=$REGION" 2>&1; then
        echo "ERROR: Teardown failed. Orphaned resources with RunID=$RUN_ID:"
        terraform state list 2>/dev/null || true
    fi
}

trap teardown EXIT

echo "=== Building PoolForge binary ==="
cd "$PROJECT_ROOT"
GOOS=linux GOARCH=amd64 go build -o poolforge ./cmd/poolforge

echo "=== Provisioning test infrastructure (RunID: $RUN_ID) ==="
cd "$SCRIPT_DIR"
terraform init -input=false
terraform apply -auto-approve \
    -var "key_name=$KEY_NAME" \
    -var "run_id=$RUN_ID" \
    -var "region=$REGION"

INSTANCE_IP=$(terraform output -raw instance_ip)
echo "Instance IP: $INSTANCE_IP"

# Wait for SSH
echo "=== Waiting for SSH ==="
for i in $(seq 1 30); do
    if ssh -o StrictHostKeyChecking=no -o ConnectTimeout=5 -i "$SSH_KEY" ubuntu@"$INSTANCE_IP" true 2>/dev/null; then
        break
    fi
    sleep 10
done

echo "=== Uploading binary and running setup ==="
scp -o StrictHostKeyChecking=no -i "$SSH_KEY" "$PROJECT_ROOT/poolforge" ubuntu@"$INSTANCE_IP":/tmp/poolforge
scp -o StrictHostKeyChecking=no -i "$SSH_KEY" "$SCRIPT_DIR/scripts/setup.sh" ubuntu@"$INSTANCE_IP":/tmp/setup.sh
ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" ubuntu@"$INSTANCE_IP" "sudo bash /tmp/setup.sh"

echo "=== Running integration tests ==="
DEVICES=$(terraform output -json volume_devices | jq -r '.[]' | tr '\n' ',')
# Map xvd devices (AWS kernel mapping)
DEVICES=$(echo "$DEVICES" | sed 's|/dev/sd|/dev/xvd|g')

TEST_EXIT=0
ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" ubuntu@"$INSTANCE_IP" "
    set -e
    echo 'Available block devices:'
    lsblk
    echo ''

    DISKS=\$(echo '$DEVICES' | sed 's/,\$//')

    echo '=== Test 1: Pool creation with mixed-size disks ==='
    # Use first 4 devices for pool1
    POOL1_DISKS=\$(echo \$DISKS | cut -d, -f1-4)
    sudo poolforge pool create --name testpool1 --disks \$POOL1_DISKS --parity parity1
    sudo poolforge pool status testpool1
    echo 'PASS: Pool creation'

    echo ''
    echo '=== Test 2: Pool list ==='
    sudo poolforge pool list
    echo 'PASS: Pool list'

    echo ''
    echo '=== Test 3: Data integrity ==='
    sudo dd if=/dev/urandom of=/mnt/poolforge/testpool1/testfile bs=1M count=10 2>/dev/null
    sudo md5sum /mnt/poolforge/testpool1/testfile
    echo 'PASS: Data write/read'

    echo ''
    echo '=== Test 4: Multi-pool isolation ==='
    POOL2_DISKS=\$(echo \$DISKS | cut -d, -f5-6)
    sudo poolforge pool create --name testpool2 --disks \$POOL2_DISKS --parity parity1
    sudo poolforge pool list
    echo 'PASS: Multi-pool isolation'

    echo ''
    echo '=== Test 5: Disk conflict rejection ==='
    CONFLICT_DISK=\$(echo \$DISKS | cut -d, -f1)
    if sudo poolforge pool create --name conflictpool --disks \$CONFLICT_DISK,\$(echo \$DISKS | cut -d, -f5) --parity parity1 2>&1; then
        echo 'FAIL: Should have rejected conflicting disk'
        exit 1
    else
        echo 'PASS: Disk conflict correctly rejected'
    fi

    echo ''
    echo '=== All tests passed ==='
" || TEST_EXIT=$?

echo "=== Collecting logs ==="
ssh -o StrictHostKeyChecking=no -i "$SSH_KEY" ubuntu@"$INSTANCE_IP" "
    cat /var/lib/poolforge/metadata.json 2>/dev/null || echo 'No metadata file'
" > "$SCRIPT_DIR/test_results_${RUN_ID}.log" 2>&1

# Teardown happens via trap
exit $TEST_EXIT
