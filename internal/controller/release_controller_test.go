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
	releasecache "github.com/suse/elemental-lifecycle-manager/internal/release"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	"github.com/suse/elemental/v3/pkg/manifest/api/core"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
)

// stubPhaseHandler is a minimal upgrade.PhaseHandler used only to populate
// Pipeline.Phases() for the LCM-failure scenario below. Its Reconcile fails loudly
// if ever invoked, since the pipeline must never run when the LCM phase fails.
type stubPhaseHandler struct {
	phase upgrade.Phase
}

func (s stubPhaseHandler) Phase() upgrade.Phase {
	return s.phase
}

func (s stubPhaseHandler) Reconcile(context.Context, *upgrade.Config) (*upgrade.PhaseStatus, error) {
	return nil, fmt.Errorf("unexpected call: pipeline phase %q must not run when LCM upgrade fails", s.phase)
}

var _ = Describe("Release Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
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
					Registry: "https://foo.bar.com",
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
			const manifestCacheName = "release-manifest-cache"
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
						Registry: "https://foo.bar.com",
					},
				}
				existingRelease := &lifecyclev1alpha1.Release{}
				err := k8sClient.Get(lcmCtx, lcmTypeNamespacedName, existingRelease)
				if err != nil && errors.IsNotFound(err) {
					Expect(k8sClient.Create(lcmCtx, lcmRelease)).To(Succeed())
				}

				By("seeding the raw manifest cache so lcmUpgradeConfig does not reach out to a registry")
				cache := &releasecache.ManifestCache{Client: k8sClient}
				Expect(cache.SetRaw(lcmCtx, elementalSystemNS, lcmRelease.Spec.Version, []byte(coreManifest))).To(Succeed())

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
					HelmClient: mockHelmClient,
				}
			})

			It("should mark Applied as Failed and skip the remaining phases when LCM upgrade fails", func() {
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

				By("checking the remaining phases were skipped rather than left Pending")
				for _, phase := range []upgrade.Phase{upgrade.PhaseOS, upgrade.PhaseKubernetes, upgrade.PhaseHelmCharts} {
					cond := apimeta.FindStatusCondition(updated.Status.Conditions, phase.ConditionType())
					Expect(cond).NotTo(BeNil(), "expected a condition for phase %q", phase)
					Expect(cond.Status).To(Equal(metav1.ConditionTrue), "phase %q", phase)
					Expect(cond.Reason).To(Equal(lifecyclev1alpha1.UpgradeSkipped), "phase %q", phase)
				}

				By("checking ManifestResolved and Applied were both marked Failed")
				manifestCond := apimeta.FindStatusCondition(updated.Status.Conditions, lifecyclev1alpha1.ConditionManifestResolved)
				Expect(manifestCond).NotTo(BeNil())
				Expect(manifestCond.Status).To(Equal(metav1.ConditionFalse))
				Expect(manifestCond.Reason).To(Equal(lifecyclev1alpha1.UpgradeFailed))

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

				cacheConfigMap := &corev1.ConfigMap{}
				err := k8sClient.Get(lcmCtx, types.NamespacedName{Name: manifestCacheName, Namespace: elementalSystemNS}, cacheConfigMap)
				if err == nil {
					Expect(k8sClient.Delete(lcmCtx, cacheConfigMap)).To(Succeed())
				}
			})
		})
	})
})
