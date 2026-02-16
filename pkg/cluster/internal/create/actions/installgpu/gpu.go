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

package installgpu

import (
	"bytes"
	"strconv"
	"strings"
	"text/template"

	"sigs.k8s.io/kind/pkg/cluster/nodes"
	"sigs.k8s.io/kind/pkg/errors"

	"sigs.k8s.io/kind/pkg/cluster/internal/create/actions"
	"sigs.k8s.io/kind/pkg/cluster/internal/providers/docker/gpu"
	"sigs.k8s.io/kind/pkg/cluster/nodeutils"
)

type action struct{}

// NewAction returns a new action for installing the NVIDIA GPU device plugin.
// This action is a no-op if GPU configuration is not present.
func NewAction() actions.Action {
	return &action{}
}

// devicePluginData holds template data for rendering the device plugin manifest.
type devicePluginData struct {
	GPUReplicas int
}

// Execute runs the action
func (a *action) Execute(ctx *actions.ActionContext) error {
	// skip if GPU is not configured
	if ctx.Config.GPU == nil {
		return nil
	}

	ctx.Status.Start("Configuring GPU support 🎮")
	defer ctx.Status.End(false)

	allNodes, err := ctx.Nodes()
	if err != nil {
		return err
	}

	internalNodes, err := nodeutils.InternalNodes(allNodes)
	if err != nil {
		return err
	}

	// refresh ldconfig on each node so the dynamic linker can find NVIDIA
	// driver libraries from the host mounts (e.g. /usr/lib/wsl/lib on WSL2)
	if err := refreshLibraryCache(internalNodes); err != nil {
		return errors.Wrap(err, "failed to refresh library cache")
	}

	// import device plugin image from baked-in tar into containerd on each node
	if err := importDevicePluginImage(internalNodes); err != nil {
		return errors.Wrap(err, "failed to import device plugin image")
	}

	// render and apply device plugin manifest
	controlPlanes, err := nodeutils.ControlPlaneNodes(allNodes)
	if err != nil {
		return err
	}
	if len(controlPlanes) == 0 {
		return errors.New("no control plane nodes found")
	}

	replicas := gpu.DefaultGPUReplicas
	if r, ok := ctx.Config.GPU.Parameters["replicas"]; ok {
		if v, err := strconv.Atoi(r); err == nil && v > 0 {
			replicas = v
		}
	}

	manifest, err := renderDevicePluginManifest(replicas)
	if err != nil {
		return errors.Wrap(err, "failed to render device plugin manifest")
	}

	if err := applyManifest(controlPlanes[0], manifest); err != nil {
		return errors.Wrap(err, "failed to apply device plugin manifest")
	}

	ctx.Status.End(true)
	return nil
}

// refreshLibraryCache runs ldconfig on each node to rebuild the dynamic linker
// cache after host GPU libraries are bind-mounted.
func refreshLibraryCache(nodes []nodes.Node) error {
	for _, node := range nodes {
		if err := node.Command("ldconfig").Run(); err != nil {
			return errors.Wrapf(err, "running ldconfig on node %s", node.String())
		}
	}
	return nil
}

// importDevicePluginImage imports the device plugin image tar baked into the
// GPU node image into containerd on each node.
func importDevicePluginImage(nodes []nodes.Node) error {
	for _, node := range nodes {
		if err := node.Command(
			"ctr", "--namespace=k8s.io", "images", "import",
			"--all-platforms", gpu.BakedDevicePluginImagePath,
		).Run(); err != nil {
			return errors.Wrapf(err, "importing device plugin image on node %s", node.String())
		}
	}
	return nil
}

// renderDevicePluginManifest renders the embedded NVIDIA device plugin template.
func renderDevicePluginManifest(replicas int) (string, error) {
	tmpl, err := template.New("nvidia-device-plugin").Parse(gpu.DevicePluginTemplate)
	if err != nil {
		return "", errors.Wrap(err, "parsing device plugin template")
	}

	data := devicePluginData{GPUReplicas: replicas}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", errors.Wrap(err, "executing device plugin template")
	}

	return buf.String(), nil
}

// applyManifest applies a YAML manifest via kubectl on the given control plane node.
func applyManifest(controlPlane nodes.Node, manifest string) error {
	cmd := controlPlane.Command(
		"kubectl",
		"--kubeconfig=/etc/kubernetes/admin.conf",
		"apply", "-f", "-",
	)
	cmd.SetStdin(strings.NewReader(manifest))
	return cmd.Run()
}
