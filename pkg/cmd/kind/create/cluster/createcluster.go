/*
Copyright 2018 The Kubernetes Authors.

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

// Package cluster implements the `create cluster` command
package cluster

import (
	"io"
	"os"
	"time"

	"github.com/spf13/cobra"
	yaml "sigs.k8s.io/yaml"

	"sigs.k8s.io/kind/pkg/apis/config/v1alpha4"
	"sigs.k8s.io/kind/pkg/cluster"
	"sigs.k8s.io/kind/pkg/cmd"
	"sigs.k8s.io/kind/pkg/errors"
	"sigs.k8s.io/kind/pkg/log"

	"sigs.k8s.io/kind/pkg/internal/cli"
	"sigs.k8s.io/kind/pkg/internal/runtime"
)

type flagpole struct {
	Name       string
	Config     string
	ImageName  string
	GPU        string
	Retain     bool
	Wait       time.Duration
	Kubeconfig string
}

// NewCommand returns a new cobra.Command for cluster creation
func NewCommand(logger log.Logger, streams cmd.IOStreams) *cobra.Command {
	flags := &flagpole{}
	cmd := &cobra.Command{
		Args:  cobra.NoArgs,
		Use:   "cluster",
		Short: "Creates a local Kubernetes cluster",
		Long:  "Creates a local Kubernetes cluster using Docker container 'nodes'",
		RunE: func(cmd *cobra.Command, args []string) error {
			cli.OverrideDefaultName(cmd.Flags())
			return runE(logger, streams, flags)
		},
	}
	cmd.Flags().StringVarP(
		&flags.Name,
		"name",
		"n",
		"",
		"cluster name, overrides KIND_CLUSTER_NAME, config (default kind)",
	)
	cmd.Flags().StringVar(
		&flags.Config,
		"config",
		"",
		"path to a kind config file",
	)
	cmd.Flags().StringVar(
		&flags.ImageName,
		"image",
		"",
		"node docker image to use for booting the cluster",
	)
	cmd.Flags().BoolVar(
		&flags.Retain,
		"retain",
		false,
		"retain nodes for debugging when cluster creation fails",
	)
	cmd.Flags().DurationVar(
		&flags.Wait,
		"wait",
		time.Duration(0),
		"wait for control plane node to be ready (default 0s)",
	)
	cmd.Flags().StringVar(
		&flags.Kubeconfig,
		"kubeconfig",
		"",
		"sets kubeconfig path instead of $KUBECONFIG or $HOME/.kube/config",
	)
	cmd.Flags().StringVar(
		&flags.GPU,
		"gpu",
		"",
		`enable GPU passthrough with the given vendor type (e.g. "nvidia")`,
	)
	return cmd
}

func runE(logger log.Logger, streams cmd.IOStreams, flags *flagpole) error {
	provider := cluster.NewProvider(
		cluster.ProviderWithLogger(logger),
		runtime.GetDefault(logger),
	)

	// handle config flag, applying --gpu overlay if set
	withConfig, err := configOption(flags.Config, flags.GPU, streams.In)
	if err != nil {
		return err
	}

	// create the cluster
	if err = provider.Create(
		flags.Name,
		withConfig,
		cluster.CreateWithNodeImage(flags.ImageName),
		cluster.CreateWithRetain(flags.Retain),
		cluster.CreateWithWaitForReady(flags.Wait),
		cluster.CreateWithKubeconfigPath(flags.Kubeconfig),
		cluster.CreateWithDisplayUsage(true),
		cluster.CreateWithDisplaySalutation(true),
	); err != nil {
		return errors.Wrap(err, "failed to create cluster")
	}

	return nil
}

// configOption converts the raw --config flag value to a cluster creation
// option matching it. it will read from stdin if the flag value is `-`.
// If gpuType is non-empty, it patches the loaded config with GPU settings.
func configOption(rawConfigFlag string, gpuType string, stdin io.Reader) (cluster.CreateOption, error) {
	// If no GPU flag, use the simple path (preserves existing behavior exactly)
	if gpuType == "" {
		if rawConfigFlag == "-" {
			raw, err := io.ReadAll(stdin)
			if err != nil {
				return nil, errors.Wrap(err, "error reading config from stdin")
			}
			return cluster.CreateWithRawConfig(raw), nil
		}
		return cluster.CreateWithConfigFile(rawConfigFlag), nil
	}

	// GPU flag is set: load config as v1alpha4, patch GPU, use V1Alpha4Config
	cfg, err := loadV1Alpha4Config(rawConfigFlag, stdin)
	if err != nil {
		return nil, err
	}

	// Apply --gpu flag if config doesn't already have GPU set
	if cfg.GPU == nil {
		cfg.GPU = &v1alpha4.GPUConfiguration{
			Type: v1alpha4.GPUType(gpuType),
			Parameters: map[string]string{
				"replicas": "4",
			},
		}
	}

	return cluster.CreateWithV1Alpha4Config(cfg), nil
}

// loadV1Alpha4Config reads and parses a kind config file as a v1alpha4.Cluster.
// If rawConfigFlag is empty, returns an empty Cluster (will get defaults).
// If rawConfigFlag is "-", reads from stdin.
func loadV1Alpha4Config(rawConfigFlag string, stdin io.Reader) (*v1alpha4.Cluster, error) {
	cfg := &v1alpha4.Cluster{}

	var raw []byte
	var err error
	switch {
	case rawConfigFlag == "":
		return cfg, nil
	case rawConfigFlag == "-":
		raw, err = io.ReadAll(stdin)
	default:
		raw, err = os.ReadFile(rawConfigFlag)
	}
	if err != nil {
		return nil, errors.Wrap(err, "error reading config")
	}

	if err := yaml.Unmarshal(raw, cfg); err != nil {
		return nil, errors.Wrap(err, "unable to decode config")
	}
	return cfg, nil
}
