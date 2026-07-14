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
	"strings"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	"github.com/suse/elemental/v3/pkg/manifest/api/core"
	"github.com/suse/elemental/v3/pkg/manifest/api/solution"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
	"k8s.io/apimachinery/pkg/types"
)

const (
	releaseVersion   = "1.0.0"
	releaseName      = "release"
	releaseNamespace = "release-namespace"
	baseOSImage      = "registry.com/foo:16.0"
	baseOSVersion    = "16.0"
	k8sVersion       = "1.35.1"
	k8sImage         = "registry.com/rke2-tar:1.35.1"
)

var (
	coreChart            = &api.HelmChart{Chart: "foo", Repository: "foo-repo"}
	solutionChart        = &api.HelmChart{Chart: "bar", Repository: "bar-repo"}
	coreChartRepo        = &api.HelmRepository{Name: "foo-repo", URL: "https://charts.foo.com"}
	solutionChartRepo    = &api.HelmRepository{Name: "bar-repo", URL: "https://charts.bar.com"}
	corePlatformManifest = &core.ReleaseManifest{
		Components: core.Components{
			OperatingSystem: &core.OperatingSystem{
				Image: core.Image{
					Base: baseOSImage,
				},
			},
			Kubernetes: &core.Kubernetes{
				Image:   k8sImage,
				Version: k8sVersion,
			},
			Helm: &api.Helm{
				Charts:       []*api.HelmChart{coreChart},
				Repositories: []*api.HelmRepository{coreChartRepo},
			},
		},
	}
	solutionExtension = &solution.ReleaseManifest{
		Components: solution.Components{
			Helm: &api.Helm{
				Charts:       []*api.HelmChart{solutionChart},
				Repositories: []*api.HelmRepository{solutionChartRepo},
			},
		},
	}
)

var _ = Describe("Configuration creation", func() {
	It("Should fail if resolved manifest is nil", func() {
		_, err := NewConfig(nil, "", types.NamespacedName{}, nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("manifest is nil"))
	})

	It("Should fail if resolved manifest is missing core platform", func() {
		_, err := NewConfig(&resolver.ResolvedManifest{}, "", types.NamespacedName{}, nil)
		Expect(err).To(HaveOccurred())
		Expect(err).To(MatchError("core platform manifest is required"))
	})

	It("Should fail for an incorrect OS image definition", func() {
		manifest := &resolver.ResolvedManifest{
			CorePlatform: &core.ReleaseManifest{
				Components: core.Components{
					OperatingSystem: &core.OperatingSystem{
						Image: core.Image{
							Base: "registry.com/foo:bar@broken@broken",
						},
					},
				},
			},
		}
		_, err := NewConfig(manifest, "", types.NamespacedName{}, nil)
		Expect(err).To(HaveOccurred())
		Expect(err.Error()).To(ContainSubstring("parsing OS image \"registry.com/foo:bar@broken@broken\": tag can only contain the characters"))
	})

	It("Should fail for unknown charts", func() {
		manifest := &resolver.ResolvedManifest{
			CorePlatform: &core.ReleaseManifest{
				Components: core.Components{
					OperatingSystem: &core.OperatingSystem{
						Image: core.Image{
							Base: baseOSImage,
						},
					},
					Kubernetes: &core.Kubernetes{
						Image:   k8sImage,
						Version: k8sVersion,
					},
					Helm: &api.Helm{
						Charts: []*api.HelmChart{
							{Chart: "chart1"},
							{Chart: "chart2"},
						},
					},
				},
			},
		}

		runtimeConfig := &RuntimeConfig{
			HelmCharts: map[string]RuntimeHelmChartConfig{
				"chart1":   {},
				"unknown1": {},
				"unknown2": {},
				"chatr2":   {}},
		}

		_, err := NewConfig(manifest, "", types.NamespacedName{}, runtimeConfig)
		Expect(err).To(HaveOccurred())

		errSubstr := "Unknown chart references: "
		Expect(err.Error()).To(ContainSubstring(errSubstr))
		_, after, _ := strings.Cut(err.Error(), errSubstr)
		unknownCharts := strings.Split(after, ", ")
		Expect(unknownCharts).To(ConsistOf("unknown1", "unknown2", "chatr2"))
	})

	It("Should construct a correct upgrade configuration", func() {
		resolvedManifest := &resolver.ResolvedManifest{CorePlatform: corePlatformManifest, SolutionExtension: solutionExtension}
		runtimeSecretValues := &RuntimeSecretValueSource{
			Name: "foo-secret",
			Keys: []string{"foo-values"},
		}
		runtimeConfig := &RuntimeConfig{
			DrainOpts: &DrainOpts{ControlPlane: false, Worker: true},
			HelmCharts: map[string]RuntimeHelmChartConfig{
				coreChart.Chart: {
					ValuesFrom: RuntimeHelmChartValuesFrom{SecretRef: runtimeSecretValues},
				},
			},
		}
		releaseNN := types.NamespacedName{Name: releaseName, Namespace: releaseNamespace}

		config, err := NewConfig(resolvedManifest, releaseVersion, releaseNN, runtimeConfig)
		Expect(err).ToNot(HaveOccurred())

		// release validation
		Expect(config.ReleaseVersion).To(Equal(releaseVersion))
		Expect(config.ReleaseNamespacedName).To(Equal(releaseNN))

		// OS validation
		osConfig := config.OS
		Expect(osConfig.DrainOpts).To(Equal(runtimeConfig.DrainOpts))
		Expect(osConfig.Image).To(Equal(baseOSImage))
		Expect(osConfig.Version).To(Equal(baseOSVersion))

		// K8s validation
		k8sConfig := config.Kubernetes
		Expect(k8sConfig.DrainOpts).To(Equal(runtimeConfig.DrainOpts))
		Expect(k8sConfig.Image).To(Equal(k8sImage))
		Expect(k8sConfig.Version).To(Equal(k8sVersion))

		// Helm validation
		Expect(config.HelmCharts).To(HaveLen(2))

		coreHelmChartConfig := &HelmChartConfig{
			Chart:      coreChart,
			Repository: coreChartRepo,
			RuntimeConfig: RuntimeHelmChartConfig{
				ValuesFrom: RuntimeHelmChartValuesFrom{
					SecretRef: runtimeSecretValues,
				},
			},
		}
		Expect(config.HelmCharts[0]).To(Equal(coreHelmChartConfig))

		solutionHelmChartConfig := &HelmChartConfig{
			Chart:      solutionChart,
			Repository: solutionChartRepo,
		}
		Expect(config.HelmCharts[1]).To(Equal(solutionHelmChartConfig))
	})
})
