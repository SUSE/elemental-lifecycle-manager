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

package reconcilers_test

import (
	"context"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	lcmChartName    = "elemental-lifecycle-manager"
	lcmCRDChartName = lcmChartName + "-crds"
	lcmChartV1      = "v0.2.0"
	lcmChartV2      = "v0.2.1"
	lcmCRDChartV1   = "v0.1.0"
	lcmCRDChartV2   = "v0.1.1"
	testJobCRDS     = "test-job-crds"
	testJobLCM      = "test-job-lcm"
)

var _ = Describe("LCMReconciler", func() {
	var (
		ctx        context.Context
		reconciler *reconcilers.LCMReconciler
		fakeClient client.Client
		mockHelm   *testutil.MockHelmClient
		scheme     *runtime.Scheme
		config     *upgrade.Config
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = testutil.NewTestScheme()
		fakeClient = testutil.NewFakeClient(scheme)
		mockHelm = testutil.NewMockHelmClient()
		reconciler = reconcilers.NewLCMReconciler(fakeClient, mockHelm)
	})

	Describe("Phase", func() {
		It("should return PhaseLCM", func() {
			Expect(reconciler.Phase()).To(Equal(upgrade.PhaseLCM))
		})
	})

	Describe("Reconcile", func() {
		Context("When no LCM charts are in the list", func() {
			It("should skip the upgrade progress", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: testChartVersion,
						Namespace:    testNamespace,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}

				chart1 := testutil.NewTestHelmChart(testChart1Name, testChartVersion)
				config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: chart1}}))
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).ToNot(HaveOccurred())
				Expect(status).ToNot(BeNil())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
				Expect(status.Message).To(Equal("Upgrade for phase \"LCM\" skipped"))
			})

		})

		Context("when LCM charts exist", func() {
			It("should skip chart not installed on cluster", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return nil, helm.ErrReleaseNotFound
				}

				lcmChart := testutil.NewTestHelmChart(lcmChartName, lcmChartV1)
				config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: lcmChart}}))
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
				Expect(status.Message).To(ContainSubstring("All LCM charts skipped"))
			})

			It("should create a HelmChart resource with Release tracking labels", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: lcmChartV1,
						Namespace:    testNamespace,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}

				lcmChart := testutil.NewTestHelmChart(lcmChartName, lcmChartV2)
				config := testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: lcmChart}}))
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).ToNot(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))

				helmChart := &helmv1.HelmChart{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name:      lcmChartName,
					Namespace: reconcilers.HelmChartNamespace,
				}, helmChart)).To(Succeed())

				Expect(helmChart.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseNameLabel, config.ReleaseNamespacedName.Name))
				Expect(helmChart.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseVersionLabel, lifecyclev1alpha1.SanitizeVersion(config.ReleaseVersion)))
			})

			It("should upgrade LCM CRDs chart before the LCM chart", func() {
				lcmChart := testutil.NewTestHelmChart(lcmChartName, lcmChartV2, testutil.WithDependencies([]api.HelmChartDependency{{Name: lcmCRDChartName, Type: api.DependencyTypeHelm}}))
				lcmCRDChart := testutil.NewTestHelmChart(lcmCRDChartName, lcmCRDChartV2)
				config := testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{
					{
						Chart: lcmChart,
					},
					{
						Chart: lcmCRDChart,
					},
				}))

				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					switch name {
					case lcmChartName:
						return &helm.ReleaseInfo{ChartVersion: lcmChartV1, Namespace: testNamespace}, nil
					case lcmCRDChartName:
						return &helm.ReleaseInfo{ChartVersion: lcmCRDChartV1, Namespace: testNamespace}, nil
					}
					return nil, helm.ErrReleaseNotFound
				}

				status, err := reconciler.Reconcile(ctx, config)
				Expect(err).ToNot(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))

				// HelmChart CR for LCM CRDs should be created first as it's a dependency of LCM chart.
				helmLCMCRDChart := &helmv1.HelmChart{}
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name:      lcmCRDChartName,
					Namespace: reconcilers.HelmChartNamespace,
				}, helmLCMCRDChart)).To(Succeed())

				// HelmChart CR for LCM chart shouldn't be created yet
				helmLCMChart := &helmv1.HelmChart{}
				err = fakeClient.Get(ctx, types.NamespacedName{Name: lcmChartName, Namespace: reconcilers.HelmChartNamespace}, helmLCMChart)
				Expect(apierrors.IsNotFound(err)).To(BeTrue())

				helmLCMCRDChart.Status.JobName = testJobCRDS
				Expect(fakeClient.Update(ctx, helmLCMCRDChart)).To(Succeed())

				job1 := testutil.NewTestJob(testJobCRDS, reconcilers.HelmChartNamespace, true)
				Expect(fakeClient.Create(ctx, job1)).To(Succeed())

				status, err = reconciler.Reconcile(ctx, config)
				Expect(err).ToNot(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(status.Message).To(Equal("LCM charts in progress (1/2 completed, 0 skipped)"))

				// Now the HelmChart CR for LCM should be created.
				Expect(fakeClient.Get(ctx, types.NamespacedName{
					Name:      lcmChartName,
					Namespace: reconcilers.HelmChartNamespace,
				}, helmLCMChart)).To(Succeed())

				helmLCMChart.Status.JobName = testJobLCM
				Expect(fakeClient.Update(ctx, helmLCMChart)).To(Succeed())

				job2 := testutil.NewTestJob(testJobLCM, reconcilers.HelmChartNamespace, true)
				Expect(fakeClient.Create(ctx, job2)).To(Succeed())

				status, err = reconciler.Reconcile(ctx, config)
				Expect(err).ToNot(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
				Expect(status.Message).To(Equal("All 2 LCM charts upgraded successfully (0 skipped)"))
			})
		})
	})
})
