# Expected Output for Snap CI Job (`snap-test`)

This document outlines the expected output for the key commands executed in the `snap-test` CI job defined in `ci.yml`. This is intended to help users and developers understand the build and deployment process of the xTeVe snap.

**Note:** The exact output for some commands (like `lxd init` or `snapcraft`) can be verbose and might vary slightly between runner environments or versions. This guide focuses on the key indicators of success.

## 1. Install snapcraft and lxd

Commands:
```
sudo apt-get update
sudo apt-get install -y qemu-kvm
sudo snap install snapcraft --classic
sudo snap install lxd
```

Expected output:
- Standard package installation messages from `apt-get` for `qemu-kvm`.
- Successful installation messages from `snap` for `snapcraft` and `lxd`.
- Example snippet (actual output will be much longer):
  ```
  ... (apt output for qemu-kvm) ...
  snap "snapcraft" installed
  snap "lxd" installed
  ```

## 2. Configure LXD Group

Commands:
```bash
sudo groupadd --system lxd || true
sudo usermod -a -G lxd $USER || true
```

Expected output:
- These commands typically don't produce output if successful unless the group or user membership is actually changed.
- `groupadd: group 'lxd' already exists` (if it exists, due to `|| true`)
- (No output if user is already in group or successfully added, due to `|| true`)
- A comment in the CI script notes that `sg lxd -c "snapcraft"` handles the new group membership for the build step.

## 3. Initialize LXD

Command: `sudo lxd init --auto`

Expected output:
- Messages indicating successful LXD initialization.
- Example snippet:
  ```
  If you're unsure, go with the defaults.
  Would you like to use LXD clustering? (yes/no) [default=no]: 
  Do you want to configure a new storage pool? (yes/no) [default=yes]: 
  Name of the new storage pool [default=default]: 
  Name of the storage backend to use (btrfs, dir, lvm, zfs) [default=zfs]: dir
  Would you like to connect to a MAAS server? (yes/no) [default=no]: 
  Would you like to create a new local network bridge? (yes/no) [default=yes]: 
  What should the new bridge be called? [default=lxdbr0]: 
  What IPv4 address should be used? (CIDR subnet notation, “auto” or “none”) [default=auto]: 
  What IPv6 address should be used? (CIDR subnet notation, “auto” or “none”) [default=auto]: 
  Would you like LXD to be available over the network? (yes/no) [default=no]: 
  Would you like stale cached images to be updated automatically? (yes/no) [default=yes]: 
  Would you like a YAML representation of your LXD configuration to be printed? (yes/no) [default=no]:
  ```
  *(Note: `--auto` might produce less interactive output, but should indicate success)*

## 4. Build the snap

Command: `sg lxd -c "snapcraft"`

Expected output:
- `snapcraft` build logs, showing progress.
- Indication of steps like `pull`, `build`, `stage`, `prime`.
- A final message indicating successful snap creation.
- Example snippet of success:
  ```
  ...
  Snapped <snap-name>_<version>_<arch>.snap
  ```
  For xTeVe, this would look something like:
  ```
  Snapped xteve_X.Y.Z_amd64.snap
  ```

## 5. Install the snap

Command: `sudo snap install --dangerous xteve*.snap`

Expected output:
- Message indicating the snap was installed.
- Example:
  ```
  xteve <version> installed
  ```

## 6. Check service status and dump logs

Commands:
```bash
echo "--- Checking xteve service status ---"
snap services xteve
echo "--- Dumping xteve service logs ---"
sudo snap logs xteve || echo "No logs yet or logs not accessible"
echo "--- Verifying xteve service is active ---"
snap services xteve | grep -E "^xteve\s+.*active"
```

Expected output:

```
--- Checking xteve service status ---
Service  Startup  Current  Notes
xteve    enabled  active   -

--- Dumping xteve service logs ---
YYYY-MM-DDTHH:MM:SSZ xteve.daemon[PID]: <Log message 1 from xteve>
YYYY-MM-DDTHH:MM:SSZ xteve.daemon[PID]: <Log message 2 from xteve>
... (more logs) ...

--- Verifying xteve service is active ---
xteve    enabled  active   -
```

- **`snap services xteve`**: Shows the `xteve` service as `enabled` and `active`.
- **`sudo snap logs xteve`**: Outputs the logs from the `xteve` service. The exact log content will vary depending on the application's activity. If the service just started, logs might be minimal. The `|| echo "No logs yet or logs not accessible"` part is a fallback in case the logs command fails for some reason (e.g., if the service hasn't written any logs yet or there are permission issues in a specific CI setup, though `sudo` should handle most permission issues).
- **`snap services xteve | grep -E "^xteve\s+.*active"`**: This command is used to programmatically check if the service is active. If it's active, it will print the line containing "xteve" and "active", and the CI step will pass. If it's not active, `grep` will not find a match and will return a non-zero exit code, causing the CI step to fail.

This documentation should help in understanding the CI process for the xTeVe snap.
