#!/usr/bin/env bash
# Prepare the ephemeral Ubuntu runner, then test the isolated read commands (issue 21).
set -euo pipefail
sudo apt-get update -qq
sudo apt-get install -y --no-install-recommends bubblewrap socat
# This is the same Ubuntu 24.04 setup as the pinned action's non-write-user path.
# It permits user namespaces on this VM. The tool still runs in a PID namespace.
if [ -f /proc/sys/kernel/apparmor_restrict_unprivileged_userns ]; then
    sudo sysctl -w kernel.apparmor_restrict_unprivileged_userns=0
fi
env -i PATH=/usr/bin:/bin bwrap --unshare-user --unshare-pid --die-with-parent \
    --ro-bind / / --dev /dev --proc /proc -- /bin/bash -ec '
    git diff HEAD...pr/head > /dev/null
    echo "Isolation check: git diff HEAD...pr/head: success"
    git show --no-patch pr/head > /dev/null
    echo "Isolation check: git show: success"
    git log -1 pr/head > /dev/null
    echo "Isolation check: git log: success"
'
