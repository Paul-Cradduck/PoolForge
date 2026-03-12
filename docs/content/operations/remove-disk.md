+++
title = "Remove Disk"
weight = 2
+++

```bash
poolforge pool remove-disk mypool --disk /dev/sda
```

## Safety Check

Before removal, PoolForge verifies:
- Enough members remain in each array for redundancy
- No data loss — RAID can tolerate the removal

If safe, the disk is failed and removed from each array it participates in. The pool continues operating in degraded mode.

{{% notice warning %}}
Removal is blocked if it would destroy data or leave an array without redundancy.
{{% /notice %}}
