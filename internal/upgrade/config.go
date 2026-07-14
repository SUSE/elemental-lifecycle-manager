/*
Copyright © 2026 SUSE LLC
SPDX-License-Identifier: Apache-2.0

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

package upgrade

import (
	"fmt"
	"maps"
	"slices"
	"strings"

	"github.com/google/go-containerregistry/pkg/name"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	"k8s.io/apimachinery/pkg/types"
)

// Config represents a complete upgrade specification for all phases.
type Config struct {
	// ReleaseNamespacedName is the name and namespace of the Release resource.
	ReleaseNamespacedName types.NamespacedName
	// ReleaseVersion is the target release version.
	ReleaseVersion string
	// OS contains all upgrade configurations related to the target operating system.
	OS *OSConfig
	// Kubernetes contains all upgrade configurations related to the target Kubernetes distribution.
	Kubernetes *KubernetesConfig
	// HelmCharts contains all target Helm charts that need to be upgraded.
	HelmCharts []*HelmChartConfig
}

// OSConfig contains configurations related to a specific operating system upgrade.
type OSConfig struct {
	// Image is the target OS image.
	Image string
	// Version is the target OS version.
	Version string
	// DrainOpts specifies which nodes should be drained before upgrading the operating system.
	DrainOpts *DrainOpts
}

// KubernetesConfig contains configurations related to a specific target Kubernetes distribution.
type KubernetesConfig struct {
	// Image is the target Kubernetes distribution image.
	Image string
	// Version is the target Kubernetes distribution version.
	Version string
	// DrainOpts specifies which nodes should be drained before upgrading the Kubernetes distribution.
	DrainOpts *DrainOpts
}

// DrainOpts contains options for draining specific node types.
type DrainOpts struct {
	// ControlPlane specifies that control plane nodes need to be drained.
	ControlPlane bool
	// Worker specifies that worker nodes need to be drained.
	Worker bool
}

// HelmChartConfig contains the configuration for a Helm Controller HelmChart resource.
type HelmChartConfig struct {
	// Chart specifies the actual chart as defined in the target release.
	Chart *api.HelmChart
	// Repository specifies the chart repository as defined in the target release.
	Repository *api.HelmRepository
	// RuntimeConfig specifies chart configuration provided by the user at runtime.
	RuntimeConfig RuntimeHelmChartConfig
}

// RuntimeConfig specifies any upgrade configuration provided by the user at runtime.
type RuntimeConfig struct {
	DrainOpts  *DrainOpts
	HelmCharts map[string]RuntimeHelmChartConfig
}

// RuntimeConfigError specifies an error caused due to invalid runtime configuration.
type RuntimeConfigError string

func (c RuntimeConfigError) Error() string { return string(c) }

type RuntimeHelmChartConfig struct {
	// Values specifies custom values provided by the user inline.
	Values *apiextensionsv1.JSON
	// ValuesFrom specifies custom values provided from a specific source (e.g. Secret).
	ValuesFrom RuntimeHelmChartValuesFrom
}

type RuntimeHelmChartValuesFrom struct {
	// SecretRef specifies a reference to a Secret where the custom chart values are provided.
	SecretRef *RuntimeSecretValueSource
}

type RuntimeSecretValueSource struct {
	// Name specifies the name of the Secret.
	Name string
	// Keys specifies the data entries containing the desired values as seen in the Secret.
	Keys []string
}

func NewConfig(manifest *resolver.ResolvedManifest, releaseVersion string, releaseNN types.NamespacedName, runtimeConfig *RuntimeConfig) (*Config, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is nil")
	}

	if manifest.CorePlatform == nil {
		return nil, fmt.Errorf("core platform manifest is required")
	}

	core := manifest.CorePlatform
	ref, err := name.NewTag(core.Components.OperatingSystem.Image.Base, name.WeakValidation)
	if err != nil {
		return nil, fmt.Errorf("parsing OS image %q: %w", core.Components.OperatingSystem.Image.Base, err)
	}

	config := &Config{
		ReleaseNamespacedName: releaseNN,
		ReleaseVersion:        releaseVersion,
		OS: &OSConfig{
			Image:     core.Components.OperatingSystem.Image.Base,
			Version:   ref.TagStr(),
			DrainOpts: runtimeConfig.DrainOpts,
		},
	}

	config.Kubernetes = &KubernetesConfig{
		Image:     core.Components.Kubernetes.Image,
		Version:   core.Components.Kubernetes.Version,
		DrainOpts: runtimeConfig.DrainOpts,
	}

	var solutionCharts *api.Helm
	if manifest.SolutionExtension != nil {
		solutionCharts = manifest.SolutionExtension.Components.Helm
	}

	charts, err := helmChartConfig(core.Components.Helm, solutionCharts, runtimeConfig.HelmCharts)
	if err != nil {
		return nil, fmt.Errorf("parsing helm chart configuration: %w", err)
	}

	config.HelmCharts = charts
	return config, nil
}

// helmChartConfig merges Helm configurations from the core and solution manifests with any runtime-defined overrides.
func helmChartConfig(core, solution *api.Helm, runtimeConfigs map[string]RuntimeHelmChartConfig) ([]*HelmChartConfig, error) {
	chartConfig := []*HelmChartConfig{}
	unprocessedRuntimeCharts := make(map[string]struct{}, len(runtimeConfigs))
	for chart := range runtimeConfigs {
		unprocessedRuntimeCharts[chart] = struct{}{}
	}

	// Add core charts and repositories
	if core != nil {
		coreRepos := make(map[string]*api.HelmRepository, len(core.Repositories))
		for _, repo := range core.Repositories {
			coreRepos[repo.Name] = repo
		}

		for _, chart := range core.Charts {
			config := &HelmChartConfig{Chart: chart}
			if repo, ok := coreRepos[chart.Repository]; ok {
				config.Repository = repo
			}

			if rc, ok := runtimeConfigs[chart.Chart]; ok {
				config.RuntimeConfig = rc
				delete(unprocessedRuntimeCharts, chart.Chart)
			}

			chartConfig = append(chartConfig, config)
		}
	}

	// Add solution charts and repositories
	if solution != nil {
		solutionRepos := make(map[string]*api.HelmRepository, len(solution.Repositories))
		for _, repo := range solution.Repositories {
			solutionRepos[repo.Name] = repo
		}

		for _, chart := range solution.Charts {
			config := &HelmChartConfig{Chart: chart}
			if repo, ok := solutionRepos[chart.Repository]; ok {
				config.Repository = repo
			}

			if rc, ok := runtimeConfigs[chart.Chart]; ok {
				config.RuntimeConfig = rc
				delete(unprocessedRuntimeCharts, chart.Chart)
			}

			chartConfig = append(chartConfig, config)
		}

	}

	if len(unprocessedRuntimeCharts) > 0 {
		// sort unknown slice to prevent status churn
		unknown := slices.Sorted(maps.Keys(unprocessedRuntimeCharts))
		return nil, RuntimeConfigError("Unknown chart references: " + strings.Join(unknown, ", "))
	}

	if len(chartConfig) == 0 {
		return nil, nil
	}

	return chartConfig, nil
}
