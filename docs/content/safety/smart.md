+++
title = "SMART Monitoring"
weight = 2
+++

PoolForge checks disk health every 5 minutes using `smartctl`.

When a SMART failure is detected:
1. An alert is sent via the configured alert channels (webhook/email)
2. The failure is logged to the dashboard
3. The disk is flagged in pool status

{{% notice info %}}
SMART monitoring requires `smartmontools` to be installed, which the installer handles automatically.
{{% /notice %}}
