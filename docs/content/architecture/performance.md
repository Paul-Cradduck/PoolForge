+++
title = "Performance"
weight = 2
+++

Tested on AWS EC2 with 12 EBS volumes (4×10GB + 4×5GB + 4×3GB), 3 RAID5 arrays → LVM → ext4:

| Test | PoolForge | Raw mdadm | Overhead |
|------|-----------|-----------|----------|
| Sequential Write | 109 MB/s | 110 MB/s | <1% |
| Sequential Read | 262 MB/s | 263 MB/s | <1% |
| Random 4K Write | 12.5 MB/s | 12.8 MB/s | ~2% |
| Random 4K Read | 49.7 MB/s | 48.8 MB/s | 0% |

LVM + ext4 adds virtually zero overhead on top of mdadm.
