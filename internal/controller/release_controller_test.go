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
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
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
		const fooBarRegistry = "https://foo.bar.com"
		var typeNamespacedName types.NamespacedName
		var defaultCtx context.Context
		var defaultManifestRetrieve func(ctx context.Context, registry, version string) (*resolver.ResolvedManifest, error)
		var defaultPipeline *upgrade.Pipeline
		var defaultReconciler *ReleaseReconciler
		var defaultRelease *lifecyclev1alpha1.Release

		release := &lifecyclev1alpha1.Release{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Release")
			defaultCtx = context.Background()
			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: "default",
			}

			defaultRelease = &lifecyclev1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: lifecyclev1alpha1.ReleaseSpec{
					Version:  "0.0.0",
					Registry: fooBarRegistry,
				}}

			err := k8sClient.Get(defaultCtx, typeNamespacedName, release)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(defaultCtx, defaultRelease)).To(Succeed())
			}

			defaultManifestRetrieve = func(_ context.Context, _, _ string) (*resolver.ResolvedManifest, error) { //nolint:unparam

				return &resolver.ResolvedManifest{
					CorePlatform: &core.ReleaseManifest{
						Components: core.Components{
							OperatingSystem: &core.OperatingSystem{},
							Kubernetes:      &core.Kubernetes{},
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

			// coreManifest declares a single LCM chart so that lcmUpgradeConfig builds a
			// non-empty upgrade.Config, which is what lets reconcileLCM reach the Helm
			// reconciler at all rather than short-circuiting to SkippedStatus.
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
					HelmClient:   mockHelmClient,
					RawManifests: make(map[string][]byte),
				}

				By("seeding the in-memory raw manifest cache so lcmUpgradeConfig does not reach out to a registry")
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

		Context("When the LCM upgrade has already succeeded", func() {
			const releaseName = "test-resource-lcm-done"
			const releaseNamespace = "default"
			const releaseVersion = "0.3.0"
			const manifestCacheName = "release-manifest-cache"

			var doneCtx context.Context
			var doneNN types.NamespacedName

			BeforeEach(func() {
				doneCtx = context.Background()
				doneNN = types.NamespacedName{Name: releaseName, Namespace: releaseNamespace}

				By("creating a Release whose LCMUpgraded condition is already True")
				release := &lifecyclev1alpha1.Release{
					ObjectMeta: metav1.ObjectMeta{Name: releaseName, Namespace: releaseNamespace},
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
				release := &lifecyclev1alpha1.Release{}
				Expect(k8sClient.Get(doneCtx, doneNN, release)).To(Succeed())
				Expect(k8sClient.Delete(doneCtx, release)).To(Succeed())

				cacheConfigMap := &corev1.ConfigMap{}
				err := k8sClient.Get(doneCtx, types.NamespacedName{Name: manifestCacheName, Namespace: releaseNamespace}, cacheConfigMap)
				if err == nil {
					Expect(k8sClient.Delete(doneCtx, cacheConfigMap)).To(Succeed())
				}
			})

			It("should skip the LCM phase and carry on with the rest of the pipeline", func() {
				var helmCalls int
				helmClient := testutil.NewMockHelmClient()
				helmClient.RetrieveReleaseFn = func(string) (*helm.ReleaseInfo, error) {
					helmCalls++
					return nil, helm.ErrReleaseNotFound
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
								},
							},
						}, nil
					},
					Pipeline: upgrade.NewPipeline(stubPhaseHandler{
						phase:  upgrade.PhaseOS,
						status: &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeSucceeded, Message: "OS upgraded"},
					}),
					HelmClient: helmClient,
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
		})
	})
})
