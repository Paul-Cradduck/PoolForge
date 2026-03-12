+++
title = "Quick Start"
weight = 3
+++

Create your first pool in three commands:

```bash
# List available disks
poolforge pool list

# Create a pool with 3 disks and single-parity redundancy
poolforge pool create --name mypool --disks /dev/sda,/dev/sdb,/dev/sdc --parity parity1

# Check pool status
poolforge pool status mypool
```

Your pool is mounted at `/mnt/poolforge/mypool` and ready to use.

{{% notice tip %}}
Use `--parity parity2` for dual-parity (RAID6) if you want to survive 2 simultaneous disk failures.
{{% /notice %}}
