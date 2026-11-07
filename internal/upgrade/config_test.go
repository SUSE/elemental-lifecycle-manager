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
	"slices"
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
	releaseVersion       = "1.0.0"
	releaseName          = "release"
	releaseNamespace     = "release-namespace"
	baseOSImage          = "registry.com/foo:16.0"
	baseOSVersion        = "16.0"
	k8sVersion           = "1.35.1"
	k8sImage             = "registry.com/rke2-tar:1.35.1"
	lcmChartName         = "elemental-lifecycle-manager"
	lcmCRDChartName      = "elemental-lifecycle-manager-crds"
	certManagerChartName = "cert-manager"
)

var (
	coreChart            = &api.HelmChart{Chart: "foo", Repository: "foo-repo", PriorityUpgrade: true} //nolint:goconst
	lcmChart             = &api.HelmChart{Chart: lcmChartName, Repository: "lcm-repo"}
	solutionChart        = &api.HelmChart{Chart: "bar", Repository: "bar-repo"} //nolint:goconst
	coreChartRepo        = &api.HelmRepository{Name: "foo-repo", URL: "https://charts.foo.com"}
	lcmChartRepo         = &api.HelmRepository{Name: "lcm-repo", URL: "https://charts.lcm.com"}
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
				Charts:       []*api.HelmChart{coreChart, lcmChart},
				Repositories: []*api.HelmRepository{coreChartRepo, lcmChartRepo},
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

	It("Should fail for charts with circular dependencies", func() {
		manifest := &resolver.ResolvedManifest{
			CorePlatform: &core.ReleaseManifest{
				Components: core.Components{
					OperatingSystem: &core.OperatingSystem{Image: core.Image{Base: baseOSImage}},
					Kubernetes:      &core.Kubernetes{Image: k8sImage, Version: k8sVersion},
					Helm: &api.Helm{
						Charts: []*api.HelmChart{
							standardChart("chart1", "chart2").Chart,
							standardChart("chart2", "chart1").Chart,
						},
					},
				},
			},
		}

		_, err := NewConfig(manifest, "", types.NamespacedName{}, &RuntimeConfig{})
		Expect(err).To(MatchError(ConfigError("circular dependency detected involving chart chart1")))
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
		Expect(config.HelmCharts[PhasePriorityHelmCharts]).To(Equal([]*HelmChartConfig{coreHelmChartConfig}))

		solutionHelmChartConfig := &HelmChartConfig{
			Chart:      solutionChart,
			Repository: solutionChartRepo,
		}
		Expect(config.HelmCharts[PhaseHelmCharts]).To(Equal([]*HelmChartConfig{solutionHelmChartConfig}))
	})
})

var _ = Describe("Self-upgrade configuration creation", func() {
	It("Should group the LCM charts", func() {
		releaseNN := types.NamespacedName{Name: releaseName, Namespace: releaseNamespace}
		charts := &api.Helm{
			Charts:       []*api.HelmChart{lcmChart, {Chart: lcmCRDChartName, Repository: lcmChartRepo.Name}},
			Repositories: []*api.HelmRepository{lcmChartRepo},
		}

		config, err := NewSelfUpgradeConfig(releaseNN, releaseVersion, charts, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.ReleaseNamespacedName).To(Equal(releaseNN))
		Expect(config.ReleaseVersion).To(Equal(releaseVersion))
		Expect(config.OS).To(BeNil())
		Expect(config.Kubernetes).To(BeNil())
		Expect(chartNames(config.HelmCharts[PhaseLCM])).To(Equal([]string{lcmChartName, lcmCRDChartName}))
		Expect(config.HelmCharts[PhaseLCM][0].Repository).To(Equal(lcmChartRepo))
	})

	It("Should order the LCM charts by dependency and drop non-LCM charts", func() {
		charts := &api.Helm{
			Charts: []*api.HelmChart{
				{Chart: lcmChartName, DependsOn: []api.HelmChartDependency{
					{Name: lcmCRDChartName, Type: api.DependencyTypeHelm},
					{Name: certManagerChartName, Type: api.DependencyTypeHelm},
				}},
				{Chart: certManagerChartName},
				{Chart: lcmCRDChartName},
			},
		}

		config, err := NewSelfUpgradeConfig(types.NamespacedName{}, releaseVersion, charts, nil)
		Expect(err).NotTo(HaveOccurred())

		Expect(config.HelmCharts).To(HaveLen(1))
		Expect(chartNames(config.HelmCharts[PhaseLCM])).To(Equal([]string{lcmCRDChartName, lcmChartName}))
	})

	It("Should contain no Helm charts if there are no LCM charts", func() {
		charts := &api.Helm{Charts: []*api.HelmChart{{Chart: certManagerChartName}}}

		config, err := NewSelfUpgradeConfig(types.NamespacedName{}, releaseVersion, charts, nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(config.HelmCharts[PhaseLCM]).To(BeEmpty())
	})
})

//nolint:goconst
var _ = Describe("Helm chart selection", func() {
	DescribeTable("filterWithDependencies",
		func(charts []*HelmChartConfig, choose func(*HelmChartConfig) bool, expected []string) {
			selected, err := filterWithDependencies(charts, choose)

			Expect(err).NotTo(HaveOccurred())
			Expect(chartNames(selected)).To(Equal(expected))
		},
		Entry("when no charts are selected",
			[]*HelmChartConfig{standardChart("foo"), standardChart("bar")},
			selectByName(),
			[]string{},
		),
		Entry("when charts and dependencies are selected",
			[]*HelmChartConfig{standardChart("foo", "bar"), standardChart("bar")},
			selectAll,
			[]string{"bar", "foo"},
		),
		Entry("when a chart with dependency is selected",
			[]*HelmChartConfig{standardChart("foo", "bar"), standardChart("bar"), standardChart("baz")},
			selectByName("foo"),
			[]string{"bar", "foo"},
		),
		Entry("when a chart with transitive dependency is selected",
			[]*HelmChartConfig{standardChart("foo", "bar"), standardChart("bar", "baz"), standardChart("baz"), standardChart("pippo")},
			selectByName("foo"),
			[]string{"baz", "bar", "foo"},
		),
		Entry("when selected charts share a dependency",
			[]*HelmChartConfig{standardChart("foo", "baz"), standardChart("bar", "baz"), standardChart("baz"), standardChart("pippo")},
			selectAll,
			[]string{"baz", "foo", "bar", "pippo"},
		),
		Entry("when dependency is not a chart",
			[]*HelmChartConfig{
				{
					Chart: &api.HelmChart{
						Chart:     "foo",
						DependsOn: []api.HelmChartDependency{{Name: "bar", Type: api.DependencyTypeExtension}},
					},
				},
				standardChart("bar"),
			},
			selectByName("foo"),
			[]string{"foo"},
		),
		Entry("when dependency is missing",
			[]*HelmChartConfig{standardChart("foo", "missing")},
			selectAll,
			[]string{"foo"},
		),
	)

	It("should fail on circular dependencies", func() {
		charts := []*HelmChartConfig{priorityChart("foo", "bar"), standardChart("bar", "foo")}

		selected, err := filterWithDependencies(charts, selectByName("foo"))
		Expect(err).To(MatchError("circular dependency detected involving chart foo"))
		Expect(selected).To(BeNil())
	})

	It("should fail on circular dependencies between standard charts", func() {
		charts := []*HelmChartConfig{
			priorityChart("foo"),
			standardChart("bar", "baz"),
			standardChart("baz", "bar"),
		}

		priority, err := filterWithDependencies(charts, isPriorityChart)
		Expect(err).NotTo(HaveOccurred())
		Expect(chartNames(priority)).To(Equal([]string{"foo"}))

		groups, err := splitHelmChartsByPriority(charts)
		Expect(err).To(MatchError(ContainSubstring("circular dependency detected involving chart bar")))
		Expect(groups).To(BeNil())
	})

	DescribeTable("Runtime priority configuration",
		func(chartPriority bool, runtimePriority *bool, expectedPriority, expectedStandard []string) {
			c := newChartConfig("foo", chartPriority)
			c.RuntimeConfig.PriorityUpgrade = runtimePriority

			groups, err := splitHelmChartsByPriority([]*HelmChartConfig{c})
			Expect(err).NotTo(HaveOccurred())
			Expect(chartNames(groups[PhasePriorityHelmCharts])).To(Equal(expectedPriority))
			Expect(chartNames(groups[PhaseHelmCharts])).To(Equal(expectedStandard))
		},
		Entry("when priority chart is not edited at runtime", true, nil, []string{"foo"}, []string{}),
		Entry("when standard chart is not edited at runtime", false, nil, []string{}, []string{"foo"}),
		Entry("when priority chart is overridden at runtime", true, new(false), []string{}, []string{"foo"}),
		Entry("when stamdard chart is overridden at runtime", false, new(true), []string{"foo"}, []string{}),
	)

	DescribeTable("priority and standard groups never share a chart",
		func(charts []*HelmChartConfig, expectedPriority, expectedStandard []string) {
			groups, err := splitHelmChartsByPriority(charts)
			Expect(err).NotTo(HaveOccurred())

			priority := chartNames(groups[PhasePriorityHelmCharts])
			standard := chartNames(groups[PhaseHelmCharts])
			Expect(priority).To(Equal(expectedPriority))
			Expect(standard).To(Equal(expectedStandard))
			for _, name := range priority {
				Expect(standard).NotTo(ContainElement(name))
			}
		},
		Entry("when a priority chart has a standard dependency",
			[]*HelmChartConfig{priorityChart("foo", "bar"), standardChart("bar"), standardChart("baz")},
			[]string{"bar", "foo"},
			[]string{"baz"},
		),
		Entry("when a standard chart has a priority dependency",
			[]*HelmChartConfig{standardChart("foo", "bar"), priorityChart("bar"), standardChart("baz")},
			[]string{"bar"},
			[]string{"foo", "baz"},
		),
		Entry("when a standard chart depends on a chart overridden to priority at runtime",
			[]*HelmChartConfig{
				standardChart("foo", "bar"),
				func() *HelmChartConfig {
					c := standardChart("bar", "baz")
					c.RuntimeConfig.PriorityUpgrade = new(true)
					return c
				}(),
				standardChart("baz"),
			},
			[]string{"baz", "bar"},
			[]string{"foo"},
		),
	)

	It("should exclude LCM charts from priority/standard chart outputs", func() {
		charts := []*HelmChartConfig{
			priorityChart(lcmCRDChartName),
			standardChart(lcmChartName),
			priorityChart("foo", lcmChartName),
			standardChart("bar", lcmCRDChartName),
		}

		groups, err := splitHelmChartsByPriority(charts)
		Expect(err).NotTo(HaveOccurred())
		Expect(chartNames(groups[PhasePriorityHelmCharts])).To(Equal([]string{"foo"}))
		Expect(chartNames(groups[PhaseHelmCharts])).To(Equal([]string{"bar"}))
	})

	It("should return nil for no charts", func() {
		groups, err := splitHelmChartsByPriority(nil)
		Expect(err).NotTo(HaveOccurred())
		Expect(groups).To(BeNil())
	})
})

func newChartConfig(name string, priority bool, helmDeps ...string) *HelmChartConfig {
	deps := make([]api.HelmChartDependency, len(helmDeps))
	for i, dep := range helmDeps {
		deps[i] = api.HelmChartDependency{Name: dep, Type: api.DependencyTypeHelm}
	}
	return &HelmChartConfig{Chart: &api.HelmChart{Chart: name, PriorityUpgrade: priority, DependsOn: deps}}
}

func priorityChart(name string, helmDeps ...string) *HelmChartConfig {
	return newChartConfig(name, true, helmDeps...)
}

func standardChart(name string, helmDeps ...string) *HelmChartConfig {
	return newChartConfig(name, false, helmDeps...)
}

func chartNames(charts []*HelmChartConfig) []string {
	names := make([]string, 0, len(charts))
	for _, c := range charts {
		names = append(names, c.Chart.GetName())
	}
	return names
}

func selectAll(*HelmChartConfig) bool { return true }

func selectByName(names ...string) func(*HelmChartConfig) bool {
	return func(c *HelmChartConfig) bool { return slices.Contains(names, c.Chart.GetName()) }
}
