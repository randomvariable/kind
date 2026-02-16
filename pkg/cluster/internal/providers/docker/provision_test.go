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

package docker

import (
	"testing"

	"sigs.k8s.io/kind/pkg/internal/apis/config"
)

func containsArg(args []string, target string) bool {
	for _, a := range args {
		if a == target {
			return true
		}
	}
	return false
}

func TestCommonArgsGPUEnabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Cluster{
		Name: "test-gpu",
		GPU: &config.GPUConfiguration{
			Type: "nvidia",
		},
	}
	args, err := commonArgs("test-gpu", cfg, "kind", []string{"test-gpu-control-plane"})
	if err != nil {
		t.Fatalf("commonArgs returned error: %v", err)
	}
	if !containsArg(args, "--gpus=all") {
		t.Errorf("expected --gpus=all in args when GPU configured without devices, got: %v", args)
	}
}

func TestCommonArgsGPUCustomDevices(t *testing.T) {
	t.Parallel()
	cfg := &config.Cluster{
		Name: "test-gpu-custom",
		GPU: &config.GPUConfiguration{
			Type:    "nvidia",
			Devices: "0,1",
		},
	}
	args, err := commonArgs("test-gpu-custom", cfg, "kind", []string{"test-gpu-custom-control-plane"})
	if err != nil {
		t.Fatalf("commonArgs returned error: %v", err)
	}
	if !containsArg(args, "--gpus=0,1") {
		t.Errorf("expected --gpus=0,1 in args when GPU.Devices=0,1, got: %v", args)
	}
}

func TestCommonArgsGPUDisabled(t *testing.T) {
	t.Parallel()
	cfg := &config.Cluster{Name: "test-no-gpu"}
	args, err := commonArgs("test-no-gpu", cfg, "kind", []string{"test-no-gpu-control-plane"})
	if err != nil {
		t.Fatalf("commonArgs returned error: %v", err)
	}
	for _, a := range args {
		if len(a) >= 6 && a[:6] == "--gpus" {
			t.Errorf("expected no --gpus flag when GPU is nil, got: %v", args)
			break
		}
	}
}
