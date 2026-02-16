/*
Copyright 2026 The Kubernetes Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package gpu

import _ "embed"

//go:embed Dockerfile.tmpl
var dockerfileTemplate string

//go:embed configure-nvidia-cdi.sh
var configureNVIDIACDIScript string

//go:embed nvidia-device-plugin.yaml
var DevicePluginTemplate string

// NvidiaContainerdPatch is a containerd config TOML patch that registers the
// NVIDIA container runtime as an additional (non-default) runtime. Workload
// pods opt in via a RuntimeClass rather than making nvidia the default for all
// containers, which avoids CDI device injection on non-GPU pods.
const NvidiaContainerdPatch = `[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia]
  runtime_type = "io.containerd.runc.v2"
[plugins."io.containerd.grpc.v1.cri".containerd.runtimes.nvidia.options]
  BinaryName = "nvidia-container-runtime"`

// DevicePluginImage is the container image used by the NVIDIA device plugin
// DaemonSet. It is preloaded into the node image to avoid pulling from an
// external registry at cluster creation time.
const DevicePluginImage = "nvcr.io/nvidia/k8s-device-plugin:v0.18.2"

// BakedDevicePluginImagePath is the path inside the custom GPU node image
// where the device plugin OCI tar is stored during the Docker build.
const BakedDevicePluginImagePath = "/opt/gpu-images/device-plugin.tar"

// DefaultGPUReplicas is the default number of GPU time-slicing replicas
// per physical GPU when the "replicas" parameter is not specified.
const DefaultGPUReplicas = 2
