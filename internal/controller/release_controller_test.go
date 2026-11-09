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

package controller

import (
	"context"
	"fmt"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	"github.com/suse/elemental/v3/pkg/manifest/api/core"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
)

// stubPhaseHandler is a minimal upgrade.PhaseHandler. When status is set Reconcile returns it;
// otherwise Reconcile fails loudly, for scenarios where the pipeline must never run.
type stubPhaseHandler struct {
	phase  upgrade.Phase
	status *upgrade.PhaseStatus
}

func (s stubPhaseHandler) Phase() upgrade.Phase {
	return s.phase
}

func (s stubPhaseHandler) Reconcile(context.Context, *upgrade.Config) (*upgrade.PhaseStatus, error) {
	if s.status != nil {
		return s.status, nil
	}
	return nil, fmt.Errorf("unexpected call: pipeline phase %q must not run in this scenario", s.phase)
}

var _ = Describe("Release Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		const defaultNamespace = "default"
		const fooBarRegistry = "https://foo.bar.com"
		var typeNamespacedName types.NamespacedName
		var defaultCtx context.Context
		var defaultManifestRetrieve func(ctx context.Context, registry, version string) (*resolver.ResolvedManifest, error)
		var defaultPipeline *upgrade.Pipeline
		var defaultReconciler *ReleaseReconciler
		var defaultRelease *lifecyclev1alpha1.Release

		release := &lifecyclev1alpha1.Release{}
		deleteReleaseAndManifestCache := func(ctx context.Context, nn types.NamespacedName) {
			release := &lifecyclev1alpha1.Release{}
			Expect(k8sClient.Get(ctx, nn, release)).To(Succeed())
			Expect(k8sClient.Delete(ctx, release)).To(Succeed())

			cacheConfigMap := &corev1.ConfigMap{}
			err := k8sClient.Get(ctx, types.NamespacedName{Name: "release-manifest-cache", Namespace: nn.Namespace}, cacheConfigMap)
			if err == nil {
				Expect(k8sClient.Delete(ctx, cacheConfigMap)).To(Succeed())
			}
		}

		addUnknownFields := func(ctx context.Context, nn types.NamespacedName) {
			patch := []byte(`{"spec":{"unsupportedField":"x","componentConfiguration":{"helm":[{"chart":"foo","unsupportedChartField":true}]}}}`)
			release := &lifecyclev1alpha1.Release{ObjectMeta: metav1.ObjectMeta{Name: nn.Name, Namespace: nn.Namespace}}
			Expect(k8sClient.Patch(ctx, release, client.RawPatch(types.MergePatchType, patch))).To(Succeed())

			release.Status.ObservedGeneration = release.Generation
			Expect(k8sClient.Status().Update(ctx, release)).To(Succeed())
		}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Release")
			defaultCtx = context.Background()
			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: defaultNamespace,
			}

			defaultRelease = &lifecyclev1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: defaultNamespace,
				},
				Spec: lifecyclev1alpha1.ReleaseSpec{
					Version:  "0.0.0",
					Registry: fooBarRegistry,
				}}

			err := k8sClient.Get(defaultCtx, typeNamespacedName, release)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(defaultCtx, defaultRelease)).To(Succeed())
			}

			// defaultManifestRetrieve returns the smallest release manifest that the upgrade config can be built from.
			defaultManifestRetrieve = func(context.Context, string, string) (*resolver.ResolvedManifest, error) { //nolint:unparam
				return &resolver.ResolvedManifest{
					CorePlatform: &core.ReleaseManifest{
						Components: core.Components{
							OperatingSystem: &core.OperatingSystem{Image: core.Image{Base: "registry.example.com/os:1.0.0"}},
							Kubernetes:      &core.Kubernetes{Version: "v1.33.0", Image: "registry.example.com/k8s"},
						},
					},
				}, nil
			}

			defaultPipeline = upgrade.NewPipeline()

			defaultReconciler = &ReleaseReconciler{
				Client:           k8sClient,
				Scheme:           k8sClient.Scheme(),
				RetrieveManifest: defaultManifestRetrieve,
				Pipeline:         defaultPipeline,
				HelmClient:       testutil.NewMockHelmClient(),
			}
		})

		AfterEach(func() {
			resource := &lifecyclev1alpha1.Release{}
			err := k8sClient.Get(defaultCtx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Release")
			Expect(k8sClient.Delete(defaultCtx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			_, err := defaultReconciler.Reconcile(defaultCtx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})

		Context("When LCM upgrade fails", func() {
			const lcmResourceName = "test-resource-lcm-failure"
			const elementalSystemNS = "elemental-system"

			var lcmCtx context.Context
			var lcmTypeNamespacedName types.NamespacedName
			var lcmRelease *lifecyclev1alpha1.Release
			var lcmReconciler *ReleaseReconciler
			var mockHelmClient *testutil.MockHelmClient

			const coreManifest = `
components:
  helm:
    charts:
      - name: elemental-lifecycle-manager
        chart: elemental-lifecycle-manager
        version: 0.2.1
        namespace: elemental-system
        repository: lcm-repo
    repositories:
      - name: lcm-repo
        url: https://example.com/charts
`

			BeforeEach(func() {
				lcmCtx = context.Background()
				lcmTypeNamespacedName = types.NamespacedName{
					Name:      lcmResourceName,
					Namespace: elementalSystemNS,
				}

				By("ensuring the elemental-system namespace exists")
				namespace := &corev1.Namespace{
					ObjectMeta: metav1.ObjectMeta{Name: elementalSystemNS},
				}
				if err := k8sClient.Get(lcmCtx, types.NamespacedName{Name: elementalSystemNS}, &corev1.Namespace{}); err != nil && errors.IsNotFound(err) {
					Expect(k8sClient.Create(lcmCtx, namespace)).To(Succeed())
				}

				By("creating the Release resource for the LCM failure scenario")
				lcmRelease = &lifecyclev1alpha1.Release{
					ObjectMeta: metav1.ObjectMeta{
						Name:      lcmResourceName,
						Namespace: elementalSystemNS,
					},
					Spec: lifecyclev1alpha1.ReleaseSpec{
						Version:  "0.2.1",
						Registry: fooBarRegistry,
					},
				}
				existingRelease := &lifecyclev1alpha1.Release{}
				err := k8sClient.Get(lcmCtx, lcmTypeNamespacedName, existingRelease)
				if err != nil && errors.IsNotFound(err) {
					Expect(k8sClient.Create(lcmCtx, lcmRelease)).To(Succeed())
				}

				By("wiring a Helm client that deterministically fails chart reconciliation")
				mockHelmClient = testutil.NewMockHelmClient()
				mockHelmClient.RetrieveReleaseFn = func(_ string) (*helm.ReleaseInfo, error) {
					return nil, fmt.Errorf("simulated helm storage failure")
				}

				// RetrieveManifest is deliberately left nil: reconcileNormal returns before ever
				// reaching getOrRetrieveManifest when the LCM phase fails.
				lcmReconciler = &ReleaseReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					Pipeline: upgrade.NewPipeline(
						stubPhaseHandler{phase: upgrade.PhaseOS},
						stubPhaseHandler{phase: upgrade.PhaseKubernetes},
						stubPhaseHandler{phase: upgrade.PhaseHelmCharts},
					),
					AutoUpgradeHandler: reconcilers.NewLCMHelmReconciler(k8sClient, mockHelmClient),
					RawManifests:       make(map[string][]byte),
				}

				By("seeding the in-memory raw manifest cache so prepareAutoUpgradeConfig does not reach out to a registry")
				imageRef := fmt.Sprintf("%s:%s", lcmRelease.Spec.Registry, lcmRelease.Spec.Version)
				lcmReconciler.RawManifests[imageRef] = []byte(coreManifest)
			})

			It("should mark Applied as Failed and leave the other conditions untouched when LCM upgrade fails", func() {
				By("initializing pending conditions on the first reconcile")
				_, err := lcmReconciler.Reconcile(lcmCtx, reconcile.Request{NamespacedName: lcmTypeNamespacedName})
				Expect(err).NotTo(HaveOccurred())

				By("reconciling again so the LCM phase actually runs and fails")
				result, err := lcmReconciler.Reconcile(lcmCtx, reconcile.Request{NamespacedName: lcmTypeNamespacedName})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(BeZero())

				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(lcmCtx, lcmTypeNamespacedName, updated)).To(Succeed())

				By("checking the LCMUpgraded condition reports the failure")
				lcmCond := apimeta.FindStatusCondition(updated.Status.Conditions, upgrade.PhaseLCM.ConditionType())
				Expect(lcmCond).NotTo(BeNil())
				Expect(lcmCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(lcmCond.Reason).To(Equal(lifecyclev1alpha1.UpgradeFailed))

				By("checking the remaining phases and ManifestResolved were left Pending")
				for _, conditionType := range []string{
					lifecyclev1alpha1.ConditionManifestResolved,
					upgrade.PhaseOS.ConditionType(),
					upgrade.PhaseKubernetes.ConditionType(),
					upgrade.PhaseHelmCharts.ConditionType(),
				} {
					cond := apimeta.FindStatusCondition(updated.Status.Conditions, conditionType)
					Expect(cond).NotTo(BeNil(), "expected a condition of type %q", conditionType)
					Expect(cond.Status).To(Equal(metav1.ConditionFalse), "condition %q", conditionType)
					Expect(cond.Reason).To(Equal(lifecyclev1alpha1.UpgradePending), "condition %q", conditionType)
				}

				By("checking Applied was marked Failed")
				appliedCond := apimeta.FindStatusCondition(updated.Status.Conditions, lifecyclev1alpha1.ConditionApplied)
				Expect(appliedCond).NotTo(BeNil())
				Expect(appliedCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(appliedCond.Reason).To(Equal(lifecyclev1alpha1.UpgradeFailed))
			})

			AfterEach(func() {
				resource := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(lcmCtx, lcmTypeNamespacedName, resource)).To(Succeed())
				By("Cleanup the specific resource instance Release")
				Expect(k8sClient.Delete(lcmCtx, resource)).To(Succeed())
			})
		})

		Context("When the LCM upgrade was completed in an earlier reconcile", func() {
			const releaseName = "test-resource-lcm-done"
			const releaseVersion = "0.3.0"

			var doneCtx context.Context
			var doneNN types.NamespacedName

			BeforeEach(func() {
				doneCtx = context.Background()
				doneNN = types.NamespacedName{Name: releaseName, Namespace: defaultNamespace}

				By("creating a Release whose LCMUpgraded condition is already True")
				release := &lifecyclev1alpha1.Release{
					ObjectMeta: metav1.ObjectMeta{Name: releaseName, Namespace: defaultNamespace},
					Spec: lifecyclev1alpha1.ReleaseSpec{
						Version:      releaseVersion,
						Registry:     fooBarRegistry,
						DisableDrain: true,
					},
				}
				Expect(k8sClient.Create(doneCtx, release)).To(Succeed())
				Expect(k8sClient.Get(doneCtx, doneNN, release)).To(Succeed())

				// Matching ObservedGeneration to Generation keeps the reconciler from resetting the conditions.
				release.Status.ObservedGeneration = release.Generation
				setCondition(release, upgrade.PhaseLCM.ConditionType(), metav1.ConditionTrue,
					lifecyclev1alpha1.UpgradeSucceeded, "LCM charts upgraded")
				initializePendingConditions(release, []upgrade.Phase{upgrade.PhaseOS})
				Expect(k8sClient.Status().Update(doneCtx, release)).To(Succeed())
			})

			AfterEach(func() {
				deleteReleaseAndManifestCache(doneCtx, doneNN)
			})

			It("should skip the LCM phase and carry on with the rest of the pipeline", func() {
				var helmCalls int
				helmClient := testutil.NewMockHelmClient()
				helmClient.RetrieveReleaseFn = func(string) (*helm.ReleaseInfo, error) {
					helmCalls++
					return nil, helm.ErrReleaseNotFound
				}

				reconciler := &ReleaseReconciler{
					Client:           k8sClient,
					Scheme:           k8sClient.Scheme(),
					RetrieveManifest: defaultManifestRetrieve,
					Pipeline: upgrade.NewPipeline(stubPhaseHandler{
						phase:  upgrade.PhaseOS,
						status: &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeSucceeded, Message: "OS upgraded"},
					}),
					AutoUpgradeHandler: reconcilers.NewLCMHelmReconciler(k8sClient, helmClient),
					HelmClient:         testutil.NewMockHelmClient(),
				}

				result, err := reconciler.Reconcile(doneCtx, reconcile.Request{NamespacedName: doneNN})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(BeZero())

				By("checking the LCM phase was not run again")
				Expect(helmCalls).To(BeZero())

				By("checking the pipeline ran to completion")
				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(doneCtx, doneNN, updated)).To(Succeed())
				Expect(updated.Status.Version).To(Equal(releaseVersion))
				for _, conditionType := range []string{
					upgrade.PhaseLCM.ConditionType(),
					upgrade.PhaseOS.ConditionType(),
					lifecyclev1alpha1.ConditionManifestResolved,
					lifecyclev1alpha1.ConditionApplied,
				} {
					Expect(apimeta.IsStatusConditionTrue(updated.Status.Conditions, conditionType)).To(BeTrue(), "condition %q", conditionType)
				}
			})

			It("should report a failed condition without running the pipeline when the release charts contain a dependency cycle", func() {
				helmDependency := func(name string) []api.HelmChartDependency {
					return []api.HelmChartDependency{{Name: name, Type: api.DependencyTypeHelm}}
				}

				reconciler := &ReleaseReconciler{
					Client: k8sClient,
					Scheme: k8sClient.Scheme(),
					RetrieveManifest: func(context.Context, string, string) (*resolver.ResolvedManifest, error) {
						return &resolver.ResolvedManifest{
							CorePlatform: &core.ReleaseManifest{
								Components: core.Components{
									OperatingSystem: &core.OperatingSystem{Image: core.Image{Base: "registry.example.com/os:1.0.0"}},
									Kubernetes:      &core.Kubernetes{Version: "v1.33.0", Image: "registry.example.com/k8s"},
									Helm: &api.Helm{
										Charts: []*api.HelmChart{
											{Chart: "foo", DependsOn: helmDependency("bar")},
											{Chart: "bar", DependsOn: helmDependency("foo")},
										},
									},
								},
							},
						}, nil
					},
					// The stub fails if the pipeline runs, which it must not.
					Pipeline:           upgrade.NewPipeline(stubPhaseHandler{phase: upgrade.PhaseOS}),
					AutoUpgradeHandler: reconcilers.NewLCMHelmReconciler(k8sClient, testutil.NewMockHelmClient()),
					HelmClient:         testutil.NewMockHelmClient(),
				}

				_, err := reconciler.Reconcile(doneCtx, reconcile.Request{NamespacedName: doneNN})
				Expect(err).NotTo(HaveOccurred())

				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(doneCtx, doneNN, updated)).To(Succeed())
				applied := apimeta.FindStatusCondition(updated.Status.Conditions, lifecyclev1alpha1.ConditionApplied)
				Expect(applied).NotTo(BeNil())
				Expect(applied.Status).To(Equal(metav1.ConditionFalse))
				Expect(applied.Reason).To(Equal(lifecyclev1alpha1.UpgradeFailed))
				Expect(applied.Message).To(ContainSubstring("circular dependency detected involving chart foo"))
			})

			It("should report a failed condition when the release contains unsupported fields", func() {
				addUnknownFields(doneCtx, doneNN)

				reconciler := &ReleaseReconciler{
					Client:           k8sClient,
					Scheme:           k8sClient.Scheme(),
					RetrieveManifest: defaultManifestRetrieve,
					Pipeline:         upgrade.NewPipeline(stubPhaseHandler{phase: upgrade.PhaseOS}),
				}

				result, err := reconciler.Reconcile(doneCtx, reconcile.Request{NamespacedName: doneNN})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(BeZero())

				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(doneCtx, doneNN, updated)).To(Succeed())
				applied := apimeta.FindStatusCondition(updated.Status.Conditions, lifecyclev1alpha1.ConditionApplied)
				Expect(applied).NotTo(BeNil())
				Expect(applied.Status).To(Equal(metav1.ConditionFalse))
				Expect(applied.Reason).To(Equal(lifecyclev1alpha1.UpgradeFailed))
				Expect(applied.Message).To(ContainSubstring(`unknown field "spec.componentConfiguration.helm[0].unsupportedChartField"`))
				Expect(applied.Message).To(ContainSubstring(`unknown field "spec.unsupportedField"`))
			})
		})

		Context("When the LCM charts are upgraded in this reconcile", func() {
			const releaseVersion = "0.3.0"
			const coreManifest = `
components:
  helm:
    charts:
      - chart: elemental-lifecycle-manager
        version: 0.3.0
`
			takeoverCtx := context.Background()
			takeoverNN := types.NamespacedName{Name: "test-resource-lcm-takeover", Namespace: defaultNamespace}
			newReconciler := func(chartVersion string) *ReleaseReconciler {
				return &ReleaseReconciler{
					Client:   k8sClient,
					Pipeline: upgrade.NewPipeline(),
					AutoUpgradeHandler: stubPhaseHandler{
						phase:  upgrade.PhaseLCM,
						status: &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeSucceeded},
					},
					RawManifests: map[string][]byte{fooBarRegistry + ":" + releaseVersion: []byte(coreManifest)},
					ChartVersion: chartVersion,
					HelmClient:   testutil.NewMockHelmClient(),
				}
			}

			BeforeEach(func() {
				release := &lifecyclev1alpha1.Release{
					ObjectMeta: metav1.ObjectMeta{Name: takeoverNN.Name, Namespace: takeoverNN.Namespace},
					Spec:       lifecyclev1alpha1.ReleaseSpec{Version: releaseVersion, Registry: fooBarRegistry},
				}
				Expect(k8sClient.Create(takeoverCtx, release)).To(Succeed())

				release.Status.ObservedGeneration = release.Generation
				Expect(k8sClient.Status().Update(takeoverCtx, release)).To(Succeed())
			})

			AfterEach(func() {
				deleteReleaseAndManifestCache(takeoverCtx, takeoverNN)
			})

			It("should not continue the upgrade while the old LCM process is still running", func() {
				reconciler := newReconciler("0.2.0")

				result, err := reconciler.Reconcile(takeoverCtx, reconcile.Request{NamespacedName: takeoverNN})
				Expect(err).NotTo(HaveOccurred())
				Expect(result.RequeueAfter).To(Equal(requeueInterval))

				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(takeoverCtx, takeoverNN, updated)).To(Succeed())

				lcmCond := apimeta.FindStatusCondition(updated.Status.Conditions, upgrade.PhaseLCM.ConditionType())
				Expect(lcmCond).NotTo(BeNil())
				Expect(lcmCond.Reason).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(lcmCond.Message).To(Equal("Waiting for a running process with 0.3.0 version"))
			})

			It("should not validate fields meant for the new LCM version while the old LCM process is still running", func() {
				addUnknownFields(takeoverCtx, takeoverNN)

				_, err := newReconciler("0.2.0").Reconcile(takeoverCtx, reconcile.Request{NamespacedName: takeoverNN})
				Expect(err).NotTo(HaveOccurred())

				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(takeoverCtx, takeoverNN, updated)).To(Succeed())
				applied := apimeta.FindStatusCondition(updated.Status.Conditions, lifecyclev1alpha1.ConditionApplied)
				Expect(applied).NotTo(BeNil())
				Expect(applied.Reason).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})

			It("should continue the upgrade once the new LCM process is running", func() {
				reconciler := newReconciler("0.3.0")
				reconciler.RetrieveManifest = defaultManifestRetrieve

				_, err := reconciler.Reconcile(takeoverCtx, reconcile.Request{NamespacedName: takeoverNN})
				Expect(err).NotTo(HaveOccurred())

				updated := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(takeoverCtx, takeoverNN, updated)).To(Succeed())
				Expect(apimeta.IsStatusConditionTrue(updated.Status.Conditions, upgrade.PhaseLCM.ConditionType())).To(BeTrue())
				Expect(updated.Status.Version).To(Equal(releaseVersion))
			})
		})
	})

	Describe("confirmTakeover", func() {
		succeeded := func() *upgrade.PhaseStatus {
			return &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeSucceeded}
		}
		lcmChart := []*upgrade.HelmChartConfig{{Chart: &api.HelmChart{Chart: upgrade.LCMChartName, Version: "0.3.0"}}}

		DescribeTable("checking whether the running LCM process is at the target version",
			func(status *upgrade.PhaseStatus, running string, charts []*upgrade.HelmChartConfig, waits bool) {
				r := &ReleaseReconciler{ChartVersion: running}
				actual := r.confirmTakeover(status, &upgrade.Config{
					HelmCharts: map[upgrade.Phase][]*upgrade.HelmChartConfig{upgrade.PhaseLCM: charts},
				})

				if !waits {
					Expect(actual).To(BeIdenticalTo(status))
					return
				}
				Expect(actual.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			},
			Entry("waits when the target version is different", succeeded(), "0.2.0", lcmChart, true),
			Entry("continues when the versions match", succeeded(), "0.3.0", lcmChart, false),
			Entry("continues when the running version is unknown", succeeded(), "", lcmChart, false),
			Entry("continues when the release has no LCM charts", succeeded(), "0.2.0", nil, false),
			Entry("ignores the CRD chart version", succeeded(), "0.3.0", []*upgrade.HelmChartConfig{
				{Chart: &api.HelmChart{Chart: upgrade.LCMCRDChartName, Version: "0.4.0"}},
				{Chart: &api.HelmChart{Chart: upgrade.LCMChartName, Version: "0.3.0"}},
			}, false),
			Entry("leaves other states unchanged", &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeInProgress},
				"0.2.0", lcmChart, false),
		)
	})
})
