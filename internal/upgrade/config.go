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
	"strings"

	"github.com/suse/elemental/v3/pkg/manifest/api"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
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
	HelmCharts *HelmChartConfig
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

// DrainOpts contains options for draining specific node types
type DrainOpts struct {
	// ControlPlane specifies that control plane nodes need to be drained
	ControlPlane bool
	// Worker specifies that worker nodes need to be drained
	Worker bool
}

// HelmChartConfig contains configuration for Helm Controller HelmChart resources.
type HelmChartConfig struct {
	// Charts is the list of Helm charts to deploy/upgrade.
	Charts []*api.HelmChart
	// Repositories is the list of Helm repositories.
	Repositories []*api.HelmRepository
}

// NewConfig constructs a release upgrade specification from the given data.
func NewConfig(manifest *resolver.ResolvedManifest, releaseVersion string, releaseNamespacedName types.NamespacedName, drainOpts *DrainOpts) (*Config, error) {
	if manifest == nil {
		return nil, fmt.Errorf("manifest is nil")
	}

	if manifest.CorePlatform == nil {
		return nil, fmt.Errorf("core platform manifest is required")
	}

	core := manifest.CorePlatform
	config := &Config{
		ReleaseNamespacedName: releaseNamespacedName,
		ReleaseVersion:        releaseVersion,
		OS: &OSConfig{
			Image:     core.Components.OperatingSystem.Image.Base,
			Version:   parseImageTag(core.Components.OperatingSystem.Image.Base),
			DrainOpts: drainOpts,
		},
	}

	config.Kubernetes = &KubernetesConfig{
		Image:     core.Components.Kubernetes.Image,
		Version:   core.Components.Kubernetes.Version,
		DrainOpts: drainOpts,
	}

	if manifest.ProductExtension == nil {
		config.HelmCharts = helmChartConfig(core.Components.Helm, nil)
	} else {
		product := manifest.ProductExtension
		config.HelmCharts = helmChartConfig(core.Components.Helm, product.Components.Helm)
	}

	return config, nil
}

// helmChartConfig merges Helm configurations from core and product manifests.
func helmChartConfig(core, product *api.Helm) *HelmChartConfig {
	config := &HelmChartConfig{
		Charts:       make([]*api.HelmChart, 0),
		Repositories: make([]*api.HelmRepository, 0),
	}

	// Add core charts and repositories
	if core != nil {
		config.Charts = append(config.Charts, core.Charts...)
		config.Repositories = append(config.Repositories, core.Repositories...)
	}

	// Add product charts and repositories
	if product != nil {
		config.Charts = append(config.Charts, product.Charts...)
		config.Repositories = append(config.Repositories, product.Repositories...)
	}

	if len(config.Charts) == 0 && len(config.Repositories) == 0 {
		return nil
	}

	return config
}

func parseImageTag(image string) string {
	i := strings.LastIndex(image, ":")

	// Find the last slash to ensure the colon we found
	// isn't just a port number in the registry URL
	lastSlash := strings.LastIndex(image, "/")

	if i == -1 || i < lastSlash {
		return "latest"
	}

	return image[i+1:]
}
