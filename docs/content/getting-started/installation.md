+++
title = "Installation"
weight = 2
+++

## Install

```bash
curl -sSL https://raw.githubusercontent.com/Paul-Cradduck/PoolForge/main/install.sh | sudo bash
```

This installs the `poolforge` binary, dependencies (`mdadm`, `lvm2`, `smartmontools`), and a systemd service.

## Configure

```bash
sudo nano /etc/poolforge.conf
```

```ini
POOLFORGE_USER=admin
POOLFORGE_PASS=yourpassword
POOLFORGE_ADDR=0.0.0.0:8080
```

## Start the Service

```bash
sudo systemctl start poolforge
```

## Uninstall

```bash
curl -sSL https://raw.githubusercontent.com/Paul-Cradduck/PoolForge/main/uninstall.sh | sudo bash
```

Removes the binary, service, and optionally config/metadata. Never touches your arrays, LVM volumes, or data.
