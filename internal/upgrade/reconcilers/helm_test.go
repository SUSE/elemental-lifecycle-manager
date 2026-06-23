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
	"go.yaml.in/yaml/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
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
				config.HelmCharts = []*upgrade.HelmChartConfig{}
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
			})
		})

		Context("when chart is valid", func() {
			var chart1 *api.HelmChart

			BeforeEach(func() {
				chart1 = testutil.NewTestHelmChart(testChart1Name, "1.0.0")
				config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: chart1}}))
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

			It("should succeed without upgrading for Helm release when HelmChart is missing", func() {
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

			It("should succeed without upgrading for HelmChart resource when no changes occur", func() {
				// Create an existing HelmChart that matches the chart name and version of the default config.
				existing := testutil.NewTestHelmChartCR(chart1.Name, reconcilers.HelmChartNamespace, chart1.Version)
				// Define a chart job so that the Job evaluation step of the chart reconciliation succeeds.
				existing.Status.JobName = testChart1Job
				Expect(fakeClient.Create(ctx, existing)).To(Succeed())

				// Define Helm release output for the created HelmChart resource.
				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{ChartVersion: testChartVersion}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
			})

			Context("when chart needs an upgrade", func() {
				const (
					valueSecretName = "values-secret"
					secretValue1    = "value1"
					secretValue2    = "value2"
				)
				BeforeEach(func() {
					valuesSecret := &corev1.Secret{
						ObjectMeta: metav1.ObjectMeta{
							Name:      valueSecretName,
							Namespace: reconcilers.HelmChartNamespace,
						},
						Data: map[string][]byte{
							secretValue1: []byte("foo: bar"),
							secretValue2: []byte("bar: baz"),
						},
					}
					Expect(fakeClient.Create(ctx, valuesSecret)).To(Succeed())
				})

				DescribeTable("should schedule upgrade on chart value change",
					func(existingValues, incomingValues string) {
						// Define a config with the incoming chart values changes.
						config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{
							{
								Chart: testutil.NewTestHelmChart(testChart1Name, testChartVersion),
								RuntimeConfig: upgrade.RuntimeHelmChartConfig{
									Values: &apiextensionsv1.JSON{Raw: []byte(incomingValues)},
								},
							},
						}))

						// Create an existing HelmChart resource with an existing set of values.
						existing := testutil.NewTestHelmChartCR(testChart1Name, reconcilers.HelmChartNamespace, testChartVersion)
						existing.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(existingValues)}
						// Define a chart job so that the Job evaluation step of the chart reconciliation succeeds.
						existing.Status.JobName = testChart1Job
						Expect(fakeClient.Create(ctx, existing)).To(Succeed())

						// Define Helm release output for the created HelmChart resource.
						mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
							return &helm.ReleaseInfo{ChartVersion: testChartVersion}, nil
						}

						status, err := reconciler.Reconcile(ctx, config)
						Expect(err).NotTo(HaveOccurred())
						Expect(status.Message).To(Equal("Helm charts in progress (0/1 completed, 0 skipped)"))
						Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
					},
					Entry("when updating a value", `{"key":"old"}`, `{"key":"updated"}`),
					Entry("when adding a value", `{"key":"value"}`, `{"key":"value","new":"value"}`),
					Entry("when removing a value", `{"key":"value","to":"remove"}`, `{"key":"value"}`),
				)

				DescribeTable("should schedule upgrade on chart value secrets change",
					func(existingSecrets []helmv1.SecretSpec, incomingSecret *upgrade.RuntimeSecretValueSource) {
						// Define a config with the incoming chart values secret changes.
						config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{
							{
								Chart: testutil.NewTestHelmChart(testChart1Name, testChartVersion),
								RuntimeConfig: upgrade.RuntimeHelmChartConfig{
									ValuesFrom: upgrade.RuntimeHelmChartValuesFrom{SecretRef: incomingSecret},
								},
							},
						}))

						// Create an existing HelmChart resource with an existing set of value secrets.
						existing := testutil.NewTestHelmChartCR(testChart1Name, reconcilers.HelmChartNamespace, testChartVersion)
						existing.Spec.ValuesSecrets = existingSecrets
						// Define a chart job so that the Job evaluation step of the chart reconciliation succeeds.
						existing.Status.JobName = testChart1Job
						Expect(fakeClient.Create(ctx, existing)).To(Succeed())

						// Define Helm release output for the created HelmChart resource.
						mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
							return &helm.ReleaseInfo{ChartVersion: testChartVersion}, nil
						}

						status, err := reconciler.Reconcile(ctx, config)
						Expect(err).NotTo(HaveOccurred())
						Expect(status.Message).To(Equal("Helm charts in progress (0/1 completed, 0 skipped)"))
						Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
					},
					Entry("when updating a value secret",
						[]helmv1.SecretSpec{{Name: valueSecretName, Keys: []string{secretValue1}}},
						&upgrade.RuntimeSecretValueSource{Name: valueSecretName, Keys: []string{secretValue2}},
					),
					Entry("when adding a value secret",
						nil,
						&upgrade.RuntimeSecretValueSource{Name: valueSecretName, Keys: []string{secretValue1}},
					),
					Entry("when removing a value secret",
						[]helmv1.SecretSpec{{Name: valueSecretName, Keys: []string{secretValue1}}},
						nil,
					),
				)

				It("should schedule upgrade on chart version change", func() {
					// Create a config that specifies the new chart version.
					chart := testutil.NewTestHelmChart(testChart1Name, "2.0.0")
					config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: chart}}))

					// Create an existing HelmChart resource with an older version.
					existing := testutil.NewTestHelmChartCR(testChart1Name, reconcilers.HelmChartNamespace, "1.0.0")
					// Define Helm release output for the created HelmChart resource.
					existing.Status.JobName = testChart1Job
					Expect(fakeClient.Create(ctx, existing)).To(Succeed())

					// Define Helm release output for the created HelmChart resource.
					mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
						return &helm.ReleaseInfo{ChartVersion: testChartVersion}, nil
					}

					status, err := reconciler.Reconcile(ctx, config)
					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				})

				It("should create a HelmChart resource and trigger upgrade when such is missing", func() {
					// Expected chart values when runtime and release values are merged together.
					expectedMergedValues := &apiextensionsv1.JSON{
						Raw: []byte(`{"extraArgs":["arg1","arg3"],"foo":{"bar":"baz"},"key":"changed-value","replicas":"3"}`),
					}

					// Values secret that will be added to the HelmChart resource.
					customValueSecret := helmv1.SecretSpec{Name: valueSecretName, Keys: []string{secretValue1}}

					// The Helm chart as seen in the release manifest.
					chart := testutil.NewTestHelmChart(testChart1Name, "2.0.0")
					chart.Values = map[string]any{
						"key":      "value2",
						"replicas": "3",
						"extraArgs": []string{
							"arg1",
							"arg3",
						},
					}

					// The Helm chart runtime configuration, as defined by the user.
					config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{
						Chart: chart,
						RuntimeConfig: upgrade.RuntimeHelmChartConfig{
							Values: &apiextensionsv1.JSON{Raw: []byte(`{"key":"changed-value","foo":{"bar":"baz"}}`)},
							ValuesFrom: upgrade.RuntimeHelmChartValuesFrom{
								SecretRef: &upgrade.RuntimeSecretValueSource{
									Name: customValueSecret.Name,
									Keys: customValueSecret.Keys,
								},
							},
						},
					}}))

					// Custom values provided to the chart when it was initially deployed.
					installTimeValues := map[string]any{"installTime": "value"}

					// Define a Helm release output that will be converted to a HelmChart resource.
					mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
						return &helm.ReleaseInfo{
							ChartVersion: "1.0.0",
							Namespace:    "default",
							Config:       installTimeValues,
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

					// Ensure that the chart version was correctly updated.
					Expect(helmChart.Spec.Version).To(Equal("2.0.0"))

					// Ensure install time custom values are not corrupted.
					expectedInstallValues, err := yaml.Marshal(installTimeValues)
					Expect(err).ToNot(HaveOccurred())
					Expect(helmChart.Spec.ValuesContent).To(MatchYAML(expectedInstallValues))

					// Ensure custom values defined in release manifest and at runtime are present.
					Expect(helmChart.Spec.Values.Raw).To(MatchJSON(expectedMergedValues.Raw))

					// Ensure custom values secret defined at runtime is present.
					Expect(helmChart.Spec.ValuesSecrets).To(Equal([]helmv1.SecretSpec{customValueSecret}))
				})

				It("should update existing HelmChart resource when it is present", func() {
					// Expected chart values when runtime and release values are merged together.
					expectedMergedValues := &apiextensionsv1.JSON{
						Raw: []byte(`{"extraArgs":["arg2","arg3"],"foo":{"bar":"baz"},"key":"changed-value","replicas":"2"}`),
					}

					// Values secret that will override the existing chart values secret.
					customValueSecret := helmv1.SecretSpec{Name: valueSecretName, Keys: []string{secretValue2}}

					// The Helm chart as seen in the release manifest.
					chart := testutil.NewTestHelmChart(testChart1Name, "2.0.0")
					chart.Values = map[string]any{
						"replicas":  "2",
						"key":       "bar",
						"extraArgs": []string{"arg2", "arg3"},
					}

					// The Helm chart runtime configuration, as defined by the user.
					config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{
						Chart: chart,
						RuntimeConfig: upgrade.RuntimeHelmChartConfig{
							Values: &apiextensionsv1.JSON{Raw: []byte(`{"key":"changed-value","foo":{"bar":"baz"}}`)},
							ValuesFrom: upgrade.RuntimeHelmChartValuesFrom{
								SecretRef: &upgrade.RuntimeSecretValueSource{
									Name: customValueSecret.Name,
									Keys: customValueSecret.Keys,
								},
							},
						},
					}}))

					// Representation of an already existing chart that needs to be upgraded.
					existing := testutil.NewTestHelmChartCR(testChart1Name, reconcilers.HelmChartNamespace, testChartVersion)
					existing.Spec.Values = &apiextensionsv1.JSON{Raw: []byte(`{"old":"config"}`)}
					existingSecretValues := helmv1.SecretSpec{Name: "foo", Keys: []string{"value3"}}
					existing.Spec.ValuesSecrets = []helmv1.SecretSpec{existingSecretValues}
					Expect(fakeClient.Create(ctx, existing)).To(Succeed())

					// The Helm release representation of the existing HelmChart resource.
					mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
						return &helm.ReleaseInfo{ChartVersion: testChartVersion}, nil
					}

					status, err := reconciler.Reconcile(ctx, config)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))

					// Verify HelmChart was updated
					updated := &helmv1.HelmChart{}
					err = fakeClient.Get(ctx, types.NamespacedName{
						Name:      testChart1Name,
						Namespace: reconcilers.HelmChartNamespace,
					}, updated)
					Expect(err).NotTo(HaveOccurred())

					// Ensure that the chart version was correctly updated.
					Expect(updated.Spec.Version).To(Equal("2.0.0"))

					// Ensure that values content was not mistakenly populated.
					Expect(updated.Spec.ValuesContent).To(BeEmpty())

					// Ensure custom values defined in release manifest and at runtime are present.
					Expect(updated.Spec.Values.Raw).To(MatchJSON(expectedMergedValues.Raw))

					// Ensure custom values secret defined at runtime is present.
					Expect(updated.Spec.ValuesSecrets).To(Equal([]helmv1.SecretSpec{customValueSecret}))
				})
			})
		})

		Context("with job status evaluation", func() {
			var chart *api.HelmChart
			var helmChart *helmv1.HelmChart

			BeforeEach(func() {
				chart = testutil.NewTestHelmChart(testChart1Name, "1.0.0")
				config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: chart}}))

				helmChart = testutil.NewTestHelmChartCR(testChart1Name, reconcilers.HelmChartNamespace, "1.0.0")
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
			chart1 := testutil.NewTestHelmChart(testChart1Name, "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: "chart2", Type: api.DependencyTypeHelm}}))
			chart2 := testutil.NewTestHelmChart("chart2", "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: testChart1Name, Type: api.DependencyTypeHelm}}))
			config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: chart1}, {Chart: chart2}}))

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
			chart1 := testutil.NewTestHelmChart(testChart1Name, "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: testChart1Name, Type: api.DependencyTypeExtension}}))
			chart2 := testutil.NewTestHelmChart("chart2", "1.0.0", testutil.WithDependencies([]api.HelmChartDependency{{Name: testChart1Name, Type: api.DependencyTypeHelm}}))
			config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: chart1}, {Chart: chart2}}))

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
			config = testutil.NewTestConfig(testutil.WithHelmChartConfig([]*upgrade.HelmChartConfig{{Chart: parent}, {Chart: dep}}))

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
