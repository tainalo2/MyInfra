package deploy

// gpuScript installs the NVIDIA driver and the container toolkit on Debian,
// then refuses the machine when the card stays invisible.
const gpuScript = `#!/bin/bash
set -eu
export DEBIAN_FRONTEND=noninteractive
if nvidia-smi -L 2>/dev/null | grep -q GPU && command -v nvidia-ctk >/dev/null 2>&1; then
  if command -v docker >/dev/null 2>&1; then
    nvidia-ctk runtime configure --runtime=docker || true
  fi
  exit 0
fi
if [ -f /etc/debian_version ]; then
  if [ -f /etc/apt/sources.list ]; then
    sed -i 's/ main$/ main contrib non-free non-free-firmware/' /etc/apt/sources.list || true
  fi
  if [ -f /etc/apt/sources.list.d/debian.sources ]; then
    if ! grep -q non-free /etc/apt/sources.list.d/debian.sources; then
      sed -i 's/Components: main/Components: main contrib non-free non-free-firmware/' /etc/apt/sources.list.d/debian.sources || true
    fi
  fi
fi
apt-get update
headers=""
if [ -d /usr/src ]; then
  headers="linux-headers-$(uname -r)"
fi
apt-get install -y $headers nvidia-driver || exit 1
if ! command -v nvidia-ctk >/dev/null 2>&1; then
  install -d -m 0755 /usr/share/keyrings
  curl -fsSL https://nvidia.github.io/libnvidia-container/gpgkey | gpg --dearmor -o /usr/share/keyrings/nvidia-container-toolkit-keyring.gpg
  curl -fsSL https://nvidia.github.io/libnvidia-container/stable/deb/nvidia-container-toolkit.list | sed 's#deb https://#deb [signed-by=/usr/share/keyrings/nvidia-container-toolkit-keyring.gpg] https://#g' > /etc/apt/sources.list.d/nvidia-container-toolkit.list
  apt-get update
  apt-get install -y nvidia-container-toolkit
fi
if command -v docker >/dev/null 2>&1; then
  nvidia-ctk runtime configure --runtime=docker || true
fi
nvidia-smi -L | grep -q GPU
`

// GPUScript is the host script that makes the card usable.
func GPUScript() string {
	return gpuScript
}

// GPUPlugin is the runtime and the device plugin. Only a node labelled
// myinfra.io/gpu with a positive count runs the plugin, and only a pod that
// limits nvidia.com/gpu receives a card.
func GPUPlugin() string {
	return `apiVersion: node.k8s.io/v1
kind: RuntimeClass
metadata:
  name: nvidia
handler: nvidia
---
apiVersion: apps/v1
kind: DaemonSet
metadata:
  name: nvidia-device-plugin
  namespace: kube-system
spec:
  selector:
    matchLabels:
      name: nvidia-device-plugin
  template:
    metadata:
      labels:
        name: nvidia-device-plugin
    spec:
      affinity:
        nodeAffinity:
          requiredDuringSchedulingIgnoredDuringExecution:
            nodeSelectorTerms:
            - matchExpressions:
              - key: myinfra.io/gpu
                operator: Gt
                values: ["0"]
      tolerations:
      - operator: Exists
      priorityClassName: system-node-critical
      containers:
      - name: nvidia-device-plugin
        image: nvcr.io/nvidia/k8s-device-plugin:v0.17.1
        env:
        - name: FAIL_ON_INIT_ERROR
          value: "false"
        securityContext:
          privileged: true
        volumeMounts:
        - name: device-plugin
          mountPath: /var/lib/kubelet/device-plugins
      volumes:
      - name: device-plugin
        hostPath:
          path: /var/lib/rancher/k3s/agent/kubelet/device-plugins
`
}
