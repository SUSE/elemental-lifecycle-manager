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
	"fmt"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testChartVersion = "1.0.0"
	testNamespace    = "default"
	testChart1Name   = "chart1"
	testChart1Job    = "chart1-job"
)

var _ = Describe("HelmReconciler", func() {
	var (
		ctx        context.Context
		reconciler *reconcilers.HelmReconciler
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
		reconciler = reconcilers.NewHelmReconciler(fakeClient, mockHelm)
	})

	Describe("Phase", func() {
		It("should return PhaseHelmCharts", func() {
			Expect(reconciler.Phase()).To(Equal(upgrade.PhaseHelmCharts))
		})
	})

	Describe("Reconcile", func() {
		Context("when HelmCharts config is nil", func() {
			It("should skip the phase", func() {
				config = testutil.NewTestConfig()
				config.HelmCharts = nil

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
				Expect(status.Message).To(ContainSubstring("HelmCharts"))
			})
		})

		Context("when charts list is empty", func() {
			It("should skip the phase", func() {
				config = testutil.NewTestConfig()
				config.HelmCharts = &upgrade.HelmChartConfig{
					Charts:       []*api.HelmChart{},
					Repositories: []*api.HelmRepository{},
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
			})
		})

		Context("with valid charts", func() {
			var chart1 *api.HelmChart

			BeforeEach(func() {
				chart1 = testutil.NewTestHelmChart("chart1", "1.0.0")
				config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{chart1}))
			})

			It("should reconcile charts successfully", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: testChartVersion,
						Namespace:    testNamespace,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				Expect(status.Message).To(Equal("All 1 Helm charts upgraded successfully (0 skipped)"))
			})

			It("should skip chart not installed on cluster", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return nil, helm.ErrReleaseNotFound
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
				Expect(status.Message).To(ContainSubstring("skipped"))
			})

			It("should return error on helm client failure", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return nil, fmt.Errorf("helm client error")
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("helm client error"))
				Expect(status).NotTo(BeNil())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeFailed))
			})

			It("should succeed without upgrading", func() {
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: testChartVersion,
						Namespace:    testNamespace,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
			})

			Context("with chart needing upgrade", func() {
				It("should create HelmChart CR", func() {
					chart := testutil.NewTestHelmChart("chart1", "2.0.0")
					config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{chart}))

					mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
						return &helm.ReleaseInfo{
							ChartVersion: "1.0.0",
							Namespace:    "default",
							Config:       map[string]any{"key": "value"},
							Revisions:    1,
						}, nil
					}

					status, err := reconciler.Reconcile(ctx, config)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))

					// Verify HelmChart CR was created
					helmChart := &helmv1.HelmChart{}
					err = fakeClient.Get(ctx, types.NamespacedName{
						Name:      testChart1Name,
						Namespace: reconcilers.HelmChartNamespace,
					}, helmChart)
					Expect(err).NotTo(HaveOccurred())
					Expect(helmChart.Spec.Version).To(Equal("2.0.0"))
				})

				It("should update existing HelmChart CR", func() {
					chart := testutil.NewTestHelmChart("chart1", "2.0.0")
					config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{chart}))

					// Create existing HelmChart with old version
					existing := testutil.NewTestHelmChartCR("chart1", reconcilers.HelmChartNamespace, "1.0.0")
					Expect(fakeClient.Create(ctx, existing)).To(Succeed())

					mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
						return &helm.ReleaseInfo{
							ChartVersion: testChartVersion,
							Namespace:    testNamespace,
							Config:       map[string]any{},
							Revisions:    1,
						}, nil
					}

					status, err := reconciler.Reconcile(ctx, config)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))

					// Verify HelmChart was updated
					updated := &helmv1.HelmChart{}
					err = fakeClient.Get(ctx, types.NamespacedName{
						Name:      "chart1",
						Namespace: reconcilers.HelmChartNamespace,
					}, updated)
					Expect(err).NotTo(HaveOccurred())
					Expect(updated.Spec.Version).To(Equal("2.0.0"))
				})
			})
		})

		Context("with job status evaluation", func() {
			var chart *api.HelmChart
			var helmChart *helmv1.HelmChart

			BeforeEach(func() {
				chart = testutil.NewTestHelmChart("chart1", "1.0.0")
				config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{chart}))

				helmChart = testutil.NewTestHelmChartCR("chart1", reconcilers.HelmChartNamespace, "1.0.0")
				Expect(fakeClient.Create(ctx, helmChart)).To(Succeed())

				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: testChartVersion,
						Namespace:    testNamespace,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}
			})

			It("should detect completed job", func() {
				helmChart.Status.JobName = testChart1Job
				Expect(fakeClient.Update(ctx, helmChart)).To(Succeed())

				job := testutil.NewTestJob(testChart1Job, reconcilers.HelmChartNamespace, true)
				Expect(fakeClient.Create(ctx, job)).To(Succeed())

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
			})

			It("should detect in-progress job", func() {
				helmChart.Status.JobName = testChart1Job
				Expect(fakeClient.Update(ctx, helmChart)).To(Succeed())

				job := testutil.NewTestJob("chart1-job", reconcilers.HelmChartNamespace, false)
				Expect(fakeClient.Create(ctx, job)).To(Succeed())

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})

			It("should detect failed job", func() {
				helmChart.Status.JobName = testChart1Job
				Expect(fakeClient.Update(ctx, helmChart)).To(Succeed())

				job := testutil.NewTestJob("chart1-job", reconcilers.HelmChartNamespace, false)
				job.Status.Conditions = []batchv1.JobCondition{
					{
						Type:   batchv1.JobFailed,
						Status: corev1.ConditionTrue,
					},
				}
				Expect(fakeClient.Create(ctx, job)).To(Succeed())

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeFailed))
			})

			It("should handle missing job name as in-progress", func() {
				// Job name is empty - upgrade hasn't started yet
				helmChart.Status.JobName = ""
				Expect(fakeClient.Update(ctx, helmChart)).To(Succeed())

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})

			It("should handle completed and cleaned up job", func() {
				// Job completed and was cleaned up - check via conditions
				helmChart.Status.JobName = testChart1Job
				Expect(fakeClient.Update(ctx, helmChart)).To(Succeed())

				// Job doesn't exist (cleaned up), but no failure condition

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
			})
		})
	})

	Describe("dependency ordering (tested indirectly)", func() {
		It("should detect circular dependencies", func() {
			chart1 := testutil.NewTestHelmChart("chart1", "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: "chart2", Type: api.DependencyTypeHelm}}))
			chart2 := testutil.NewTestHelmChart("chart2", "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: "chart1", Type: api.DependencyTypeHelm}}))
			config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{chart1, chart2}))

			// Mock both charts as installed
			mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
				return &helm.ReleaseInfo{
					ChartVersion: testChartVersion,
					Namespace:    testNamespace,
					Config:       map[string]any{},
					Revisions:    1,
				}, nil
			}

			status, err := reconciler.Reconcile(ctx, config)

			Expect(err).To(HaveOccurred())
			Expect(err.Error()).To(ContainSubstring("circular dependency"))
			Expect(status).NotTo(BeNil())
			Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeFailed))

		})

		It("should not error when sysext dependency has same name as helm chart", func() {
			chart1 := testutil.NewTestHelmChart("chart1", "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: "chart1", Type: api.DependencyTypeExtension}}))
			chart2 := testutil.NewTestHelmChart("chart2", "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: "chart1", Type: api.DependencyTypeHelm}}))
			config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{chart1, chart2}))

			// Mock both charts as installed
			mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
				return &helm.ReleaseInfo{
					ChartVersion: testChartVersion,
					Namespace:    testNamespace,
					Config:       map[string]any{},
					Revisions:    1,
				}, nil
			}

			status, err := reconciler.Reconcile(ctx, config)

			Expect(err).ToNot(HaveOccurred())
			Expect(status).ToNot(BeNil())
			Expect(status.Message).To(Equal("All 2 Helm charts upgraded successfully (0 skipped)"))
			Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
		})

		It("should process dependencies before dependents", func() {
			dep := testutil.NewTestHelmChart("dependency", "1.0.0")
			parent := testutil.NewTestHelmChart("parent", "2.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: "dependency", Type: api.DependencyTypeHelm}}))
			config = testutil.NewTestConfig(testutil.WithHelmCharts([]*api.HelmChart{parent, dep}))

			processedCharts := []string{}
			mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
				processedCharts = append(processedCharts, name)
				return &helm.ReleaseInfo{
					ChartVersion: testChartVersion,
					Namespace:    testNamespace,
					Config:       map[string]any{},
					Revisions:    1,
				}, nil
			}

			status, err := reconciler.Reconcile(ctx, config)

			Expect(err).NotTo(HaveOccurred())
			Expect(status).NotTo(BeNil())
			// Dependency should be processed first
			Expect(processedCharts[0]).To(Equal("dependency"))
			Expect(processedCharts[1]).To(Equal("parent"))
		})
	})
})
