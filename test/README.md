# PoolForge Test Infrastructure

## Overview

Automated AWS EC2+EBS setup for manual validation testing. Scripts handle provisioning and teardown; testing is done manually via SSH, CLI, and web portal.

## Prerequisites

- AWS CLI configured with credentials (`~/.aws/credentials`)
- SSH key at `~/.ssh/poolforge-test` (or set `PF_SSH_KEY`)
- Region: `us-east-2` (or set `PF_REGION`)

## Scripts

### `test/aws-up.sh` — Provision Test Environment

Creates EC2 instance(s) and EBS volumes, deploys the PoolForge binary.

```bash
# Single node (phases 1-5)
./test/aws-up.sh --disks 4x10,4x5,4x3

# Two nodes (phase 6 replication testing)
./test/aws-up.sh --nodes 2 --disks 4x10,4x5,4x3
```

What it does:
1. Creates EC2 instance(s) (Ubuntu 24.04, t3.medium)
2. Waits for instance(s) to be running + SSH ready
3. Creates and attaches EBS volumes per `--disks` spec
4. Builds `poolforge` binary (GOOS=linux GOARCH=amd64)
5. SCPs binary to instance(s)
6. Runs `install.sh` on instance(s)
7. Writes connection info to `/tmp/pf-test-env.json`

Output:
```json
{
  "region": "us-east-2",
  "nodes": [
    {
      "instance_id": "i-0abc123",
      "public_ip": "18.190.1.1",
      "volumes": ["vol-aaa", "vol-bbb", "vol-ccc"],
      "disks": ["/dev/nvme1n1", "/dev/nvme2n1", "/dev/nvme3n1"]
    }
  ]
}
```

### `test/aws-down.sh` — Tear Down

Destroys all resources created by `aws-up.sh`.

```bash
./test/aws-down.sh
```

What it does:
1. Reads `/tmp/pf-test-env.json`
2. Terminates all EC2 instances
3. Waits for termination
4. Deletes all EBS volumes
5. Removes `/tmp/pf-test-env.json`

### `test/aws-ssh.sh` — Quick SSH

```bash
# SSH to node 0 (default)
./test/aws-ssh.sh

# SSH to node 1
./test/aws-ssh.sh 1
```

### `test/aws-deploy.sh` — Rebuild and Redeploy

Rebuilds the binary and deploys to all running test nodes without recreating infrastructure.

```bash
./test/aws-deploy.sh
```

## Disk Spec Format

`--disks` accepts a comma-separated list of `COUNTxSIZE` pairs (size in GB):

```
4x10,4x5,4x3     → 4 × 10GB + 4 × 5GB + 4 × 3GB = 12 disks
3x20              → 3 × 20GB = 3 disks
2x10,1x50         → 2 × 10GB + 1 × 50GB = 3 disks
```

## Environment Variables

| Variable | Default | Description |
|----------|---------|-------------|
| `PF_REGION` | `us-east-2` | AWS region |
| `PF_SSH_KEY` | `~/.ssh/poolforge-test` | SSH private key path |
| `PF_INSTANCE_TYPE` | `t3.medium` | EC2 instance type |
| `PF_AMI` | auto (Ubuntu 24.04) | AMI ID override |
| `PF_SG` | auto-created | Security group ID |
| `PF_SUBNET` | default VPC | Subnet ID |

## Manual Testing Workflow

```
1. ./test/aws-up.sh --disks 4x10,4x5,4x3
2. ./test/aws-ssh.sh
3. (manual testing on instance)
4. (fix code locally if needed)
5. ./test/aws-deploy.sh
6. (re-test)
7. ./test/aws-down.sh
```
