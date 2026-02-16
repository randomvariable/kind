#!/usr/bin/env bash
# Configure NVIDIA CDI (Container Device Interface) for GPU support inside
# kind node containers. Runs after cluster creation when host GPU devices
# and driver libraries are bind-mounted into the node.
#
# On WSL2, nvidia-ctk's auto-generated CDI spec is incomplete:
#   - Missing libdxcore.so (the DXG bridge library)
#   - Only has device "all" but the k8s device plugin assigns by UUID
# This script regenerates a complete spec and switches the nvidia runtime
# to CDI mode for reliable GPU injection.
set -euo pipefail

# Mount debugfs and tracefs if not already mounted.
# Required for eBPF tracepoints (e.g. parca-agent profiling).
mount -t debugfs debugfs /sys/kernel/debug 2>/dev/null || true
mount -t tracefs tracefs /sys/kernel/tracing 2>/dev/null || true

# Exit early if nvidia-ctk is not available.
if ! command -v nvidia-ctk >/dev/null 2>&1; then
    echo "nvidia-ctk not found, skipping CDI configuration"
    exit 0
fi

mkdir -p /etc/cdi

# Generate base CDI spec from nvidia-ctk (auto-detects WSL mode).
nvidia-ctk cdi generate --output=/etc/cdi/nvidia.yaml 2>/dev/null || true

# On WSL2 (/dev/dxg present), patch the generated CDI spec to include
# driver store mounts, libdxcore.so, and per-GPU UUID device entries.
if [ -f /etc/cdi/nvidia.yaml ] && [ -e /dev/dxg ]; then
    # Extract GPU UUID from nvidia-container-cli info.
    gpu_uuid=$(nvidia-container-cli info 2>/dev/null | grep "GPU UUID" | awk '{print $NF}') || true

    # Find the WSL driver store path (prefer the nv_dispi driver).
    driver_store=$(find /usr/lib/wsl/drivers -maxdepth 1 -name "nv_dispi*" -type d 2>/dev/null | head -1) || true

    # Find libdxcore.so (Docker injects it into the container).
    libdxcore=$(find /usr/lib -name "libdxcore.so" -type f 2>/dev/null | head -1) || true

    if [ -n "${driver_store}" ]; then
        # Build mount entries for all files in the driver store.
        mount_entries=""
        for f in "${driver_store}"/*; do
            [ -f "$f" ] || continue
            mount_entries="${mount_entries}
        - hostPath: ${f}
          containerPath: ${f}
          options: [ro, nosuid, nodev, rbind, rprivate]"
        done

        # Add libdxcore.so mount if found.
        ldcache_extra=""
        if [ -n "${libdxcore}" ]; then
            mount_entries="${mount_entries}
        - hostPath: ${libdxcore}
          containerPath: ${libdxcore}
          options: [ro, nosuid, nodev, rbind, rprivate]"
            ldcache_extra="
            - --folder
            - $(dirname "${libdxcore}")"
        fi

        # Build device entries: "all", "0", and the GPU UUID.
        device_entries="    - name: all
      containerEdits:
        deviceNodes:
            - path: /dev/dxg
    - name: \"0\"
      containerEdits:
        deviceNodes:
            - path: /dev/dxg"

        if [ -n "${gpu_uuid}" ]; then
            device_entries="${device_entries}
    - name: ${gpu_uuid}
      containerEdits:
        deviceNodes:
            - path: /dev/dxg"
        fi

        cat > /etc/cdi/nvidia.yaml <<CDIEOF
---
cdiVersion: 0.5.0
kind: nvidia.com/gpu
devices:
${device_entries}
containerEdits:
    env:
        - NVIDIA_VISIBLE_DEVICES=void
    hooks:
        - hookName: createContainer
          path: /usr/bin/nvidia-cdi-hook
          args:
            - nvidia-cdi-hook
            - create-symlinks
            - --link
            - ${driver_store}/nvidia-smi::/usr/bin/nvidia-smi
          env:
            - NVIDIA_CTK_DEBUG=false
        - hookName: createContainer
          path: /usr/bin/nvidia-cdi-hook
          args:
            - nvidia-cdi-hook
            - update-ldcache
            - --folder
            - ${driver_store}${ldcache_extra}
          env:
            - NVIDIA_CTK_DEBUG=false
    mounts:${mount_entries}
CDIEOF
        echo "WSL2 CDI spec regenerated with driver store and libdxcore"
    fi
fi

# Switch nvidia-container-runtime to CDI mode. Legacy mode has incomplete
# WSL2 support (missing libdxcore.so injection).
if [ -f /etc/nvidia-container-runtime/config.toml ]; then
    sed -i 's/^mode = "auto"/mode = "cdi"/' \
        /etc/nvidia-container-runtime/config.toml
    echo "nvidia-container-runtime switched to CDI mode"
fi

echo "NVIDIA CDI configuration complete"
