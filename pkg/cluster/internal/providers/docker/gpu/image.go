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

import (
	"bytes"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"text/template"

	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/exec"
	"sigs.k8s.io/kind/pkg/log"

	"sigs.k8s.io/kind/pkg/internal/apis/config"
	"sigs.k8s.io/kind/pkg/internal/cli"
)

// imageTagSeparatorParts is the expected number of parts when splitting an
// image reference on ":".
const imageTagSeparatorParts = 2

// dockerfileData holds template data for rendering the Dockerfile.
type dockerfileData struct {
	BaseImage         string
	DevicePluginImage string
	Platform          string
}

// BuildNodeImage builds a custom kindest/node image with NVIDIA GPU toolkit
// support. It renders the embedded Dockerfile template, writes the CDI
// configuration script, and runs docker build with BuildKit. On success it
// updates all node images in cfg to the new GPU image tag.
func BuildNodeImage(logger log.Logger, status *cli.Status, cfg *config.Cluster) error {
	baseImage := resolveBaseImage(cfg)
	imageTag := deriveGPUImageTag(baseImage)

	// check if the image already exists locally
	if imageExists(imageTag) {
		logger.V(0).Infof("GPU node image %s already exists, skipping build", imageTag)
		updateNodeImages(cfg, imageTag)
		return nil
	}

	status.Start("Building GPU node image 🔧")
	var buildErr error
	defer func() { status.End(buildErr == nil) }()

	logger.V(0).Infof("Building GPU node image %s from base %s", imageTag, baseImage)

	dockerfile, err := generateDockerfile(baseImage)
	if err != nil {
		buildErr = errors.Wrap(err, "failed to generate GPU Dockerfile")
		return buildErr
	}

	buildDir, err := os.MkdirTemp("", "kind-gpu-build-*")
	if err != nil {
		buildErr = errors.Wrap(err, "failed to create GPU build directory")
		return buildErr
	}
	defer os.RemoveAll(buildDir) //nolint:errcheck // best-effort cleanup

	// write Dockerfile
	if err := os.WriteFile(filepath.Join(buildDir, "Dockerfile"), []byte(dockerfile), 0600); err != nil {
		buildErr = errors.Wrap(err, "failed to write GPU Dockerfile")
		return buildErr
	}

	// write CDI configuration script
	if err := os.WriteFile(filepath.Join(buildDir, "configure-nvidia-cdi.sh"), []byte(configureNVIDIACDIScript), 0600); err != nil {
		buildErr = errors.Wrap(err, "failed to write CDI configuration script")
		return buildErr
	}

	// run docker build with BuildKit
	if err := runDockerBuild(buildDir, baseImage, imageTag); err != nil {
		buildErr = errors.Wrap(err, "failed to build GPU node image")
		return buildErr
	}

	logger.V(0).Infof("Successfully built GPU node image: %s", imageTag)

	// update all node images to use the GPU image
	updateNodeImages(cfg, imageTag)

	return nil
}

// resolveBaseImage returns the image from the first node in the cluster config.
func resolveBaseImage(cfg *config.Cluster) string {
	if len(cfg.Nodes) > 0 && cfg.Nodes[0].Image != "" {
		return cfg.Nodes[0].Image
	}
	return ""
}

// deriveGPUImageTag transforms "name:tag" into "name-gpu:tag".
// Any digest (@sha256:...) is stripped since docker build tags cannot contain digests.
func deriveGPUImageTag(baseImage string) string {
	// strip digest if present (e.g. "image:tag@sha256:abc..." -> "image:tag")
	ref := baseImage
	if i := strings.Index(ref, "@"); i != -1 {
		ref = ref[:i]
	}
	parts := strings.SplitN(ref, ":", imageTagSeparatorParts)
	if len(parts) == imageTagSeparatorParts {
		return parts[0] + "-gpu:" + parts[1]
	}
	return ref + "-gpu"
}

// imageExists checks if a docker image exists locally.
func imageExists(image string) bool {
	return exec.Command("docker", "inspect", "--type=image", image).Run() == nil
}

// generateDockerfile renders the embedded Dockerfile template.
func generateDockerfile(baseImage string) (string, error) {
	tmpl, err := template.New("Dockerfile").Parse(dockerfileTemplate)
	if err != nil {
		return "", errors.Wrap(err, "parsing dockerfile template")
	}

	data := dockerfileData{
		BaseImage:         baseImage,
		DevicePluginImage: DevicePluginImage,
		Platform:          runtime.GOOS + "/" + runtime.GOARCH,
	}

	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, data); err != nil {
		return "", errors.Wrap(err, "executing dockerfile template")
	}

	return buf.String(), nil
}

// runDockerBuild executes `docker build` with BuildKit enabled.
func runDockerBuild(buildDir, baseImage, imageTag string) error {
	cmd := exec.Command("docker",
		"build",
		"--build-arg", "BASE_IMAGE="+baseImage,
		"-t", imageTag,
		buildDir,
	)
	// SetEnv replaces the entire environment, so we need to include the
	// current environment plus DOCKER_BUILDKIT=1.
	cmd.SetEnv(append(os.Environ(), "DOCKER_BUILDKIT=1")...)
	return cmd.Run()
}

// updateNodeImages sets the image on all nodes to the GPU image tag.
func updateNodeImages(cfg *config.Cluster, imageTag string) {
	for i := range cfg.Nodes {
		cfg.Nodes[i].Image = imageTag
	}
}
