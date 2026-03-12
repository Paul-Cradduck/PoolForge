+++
title = "Fail Disk"
weight = 3
+++

Simulate a disk failure for testing:

```bash
poolforge pool fail-disk mypool --disk /dev/sda
```

Marks the disk as failed in all arrays it belongs to. Useful for testing rebuild procedures before a real failure occurs.
