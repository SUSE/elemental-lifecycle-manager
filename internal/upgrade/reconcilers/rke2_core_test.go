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
	"encoding/json"
	"fmt"
	"time"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testSnapshotName   = "rke2-packaged-components-snapshot"
	testChartURL       = "helm.cattle.io/chart-url"
	testOwnerName      = "objectset.rio.cattle.io/owner-name"
	testOwnerGVK       = "objectset.rio.cattle.io/owner-gvk"
	testAddonGVK       = "k3s.cattle.io/v1, Kind=Addon"
	testRKE2CoreDNS    = "rke2-coredns"
	testRKE2CoreDNSJob = "rke2-coredns-job"
	testKubeSystemNS   = "kube-system"
	testNewContent     = "new-content"
	testExampleChart   = "https://example.com/chart"
)

var _ = Describe("RKE2PackagedComponentsHandler", func() {
	var (
		ctx         context.Context
		handler     *reconcilers.RKE2PackagedComponentsHandler
		fakeClient  client.Client
		mockHelm    *testutil.MockHelmClient
		scheme      *runtime.Scheme
		config      *upgrade.Config
		findCompsFn func(ctx context.Context, c client.Client) (map[string]helmv1.HelmChart, error)
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = testutil.NewTestScheme()
		fakeClient = testutil.NewFakeClient(scheme)
		mockHelm = testutil.NewMockHelmClient()
		config = testutil.NewTestConfig(testutil.WithKubernetes("registry.example.com/k8s:v1.30.0", "v1.30.0"))

		// Create at least one node (required for snapshot creation)
		node := testutil.NewTestNode("node1", true, testutil.WithVersion("v1.30.0"))
		Expect(fakeClient.Create(ctx, node)).To(Succeed())

		// Default findComponents function returns empty map
		findCompsFn = func(ctx context.Context, c client.Client) (map[string]helmv1.HelmChart, error) {
			return map[string]helmv1.HelmChart{}, nil
		}

		handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)
	})

	Describe("GenerateSnapshot", func() {
		Context("when snapshot does not exist", func() {
			It("should create & populate snapshot with current component state", func() {
				// Create a packaged component HelmChart
				chart := testutil.NewTestHelmChartCR("rke2-coredns", "kube-system", "1.0.0")
				chart.Annotations = map[string]string{
					testChartURL:  testExampleChart,
					testOwnerName: testRKE2CoreDNS,
					testOwnerGVK:  testAddonGVK,
				}
				chart.Spec.ChartContent = "some-content"
				Expect(fakeClient.Create(ctx, chart)).To(Succeed())

				//nolint:unparam
				findCompsFn = func(_ context.Context, _ client.Client) (map[string]helmv1.HelmChart, error) {
					return map[string]helmv1.HelmChart{
						testRKE2CoreDNS: *chart,
					}, nil
				}
				handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)

				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: testChartVersion,
						Namespace:    testKubeSystemNS,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}

				snapshot, err := handler.GenerateSnapshot(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(snapshot).NotTo(BeNil())
				Expect(snapshot.SourceKubernetesVersion).To(Equal("v1.30.0"))
				Expect(snapshot.Charts).To(HaveLen(1))
				Expect(snapshot.Charts[0].Name).To(Equal("rke2-coredns"))
				Expect(snapshot.Charts[0].ChartVersion).To(Equal("1.0.0"))

				// Verify ConfigMap was created
				cm := &corev1.ConfigMap{}
				err = fakeClient.Get(ctx, types.NamespacedName{
					Name:      testSnapshotName,
					Namespace: config.ReleaseNamespacedName.Namespace,
				}, cm)
				Expect(err).NotTo(HaveOccurred())
				Expect(cm.Data).NotTo(BeEmpty())

				// Verify we can parse the chart snapshot back
				var chartSnapshot reconcilers.PackagedComponentChartSnapshot
				err = json.Unmarshal([]byte(cm.Data["rke2-coredns"]), &chartSnapshot)
				Expect(err).NotTo(HaveOccurred())
				Expect(chartSnapshot.Name).To(Equal("rke2-coredns"))
			})
		})

		Context("when snapshot exists for same release", func() {
			var createdSnapshot, receivedSnapshot *reconcilers.PackagedComponentsSnapshot
			var err error

			BeforeEach(func() {
				// Create existing snapshot
				createdSnapshot, err = handler.GenerateSnapshot(ctx, config)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should return existing snapshot", func() {
				// Get the snapshot again
				receivedSnapshot, err = handler.GenerateSnapshot(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(receivedSnapshot).NotTo(BeNil())
				Expect(createdSnapshot).To(Equal(receivedSnapshot))

				// Verify only one ConfigMap exists
				cmList := &corev1.ConfigMapList{}
				err = fakeClient.List(ctx, cmList, client.InNamespace(config.ReleaseNamespacedName.Namespace))
				Expect(err).NotTo(HaveOccurred())
				Expect(cmList.Items).To(HaveLen(1))
			})
		})

		Context("when snapshot exists for different release", func() {
			BeforeEach(func() {
				// Create snapshot for old release
				oldConfig := testutil.NewTestConfig(testutil.WithKubernetes("registry.example.com/k8s:v1.29.0", "v1.29.0"))
				oldConfig.ReleaseNamespacedName = config.ReleaseNamespacedName
				oldConfig.ReleaseVersion = "v0.9.0"
				_, err := handler.GenerateSnapshot(ctx, oldConfig)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should delete old snapshot and create new one", func() {
				config.ReleaseVersion = "v1.0.0"

				snapshot, err := handler.GenerateSnapshot(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(snapshot).NotTo(BeNil())
				Expect(snapshot.SourceKubernetesVersion).To(Equal("v1.30.0"))
			})
		})

		Context("error handling", func() {
			It("should handle component discovery errors", func() {
				findCompsFn = func(_ context.Context, _ client.Client) (map[string]helmv1.HelmChart, error) {
					return nil, fmt.Errorf("discovery failed")
				}
				handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)

				snapshot, err := handler.GenerateSnapshot(ctx, config)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("discovery failed"))
				Expect(snapshot).To(BeNil())
			})

			It("should handle helm client errors", func() {
				chart := testutil.NewTestHelmChartCR("rke2-coredns", "kube-system", "1.0.0")
				chart.Annotations = map[string]string{
					testChartURL:  testExampleChart,
					testOwnerName: testRKE2CoreDNS,
					testOwnerGVK:  testAddonGVK,
				}
				Expect(fakeClient.Create(ctx, chart)).To(Succeed())

				//nolint:unparam
				findCompsFn = func(_ context.Context, _ client.Client) (map[string]helmv1.HelmChart, error) {
					return map[string]helmv1.HelmChart{testRKE2CoreDNS: *chart}, nil
				}
				handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)

				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return nil, fmt.Errorf("helm error")
				}

				snapshot, err := handler.GenerateSnapshot(ctx, config)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("helm error"))
				Expect(snapshot).To(BeNil())
			})
		})

		It("should handle snapshot deletion", func() {
			// Create snapshot
			_, err := handler.GenerateSnapshot(ctx, config)
			Expect(err).NotTo(HaveOccurred())

			// Verify it exists
			cm := &corev1.ConfigMap{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      testSnapshotName,
				Namespace: config.ReleaseNamespacedName.Namespace,
			}, cm)
			Expect(err).NotTo(HaveOccurred())

			// Create new config with different version (triggers deletion)
			newConfig := testutil.NewTestConfig(testutil.WithKubernetes("registry.example.com/k8s:v1.31.0", "v1.31.0"))
			newConfig.ReleaseNamespacedName = config.ReleaseNamespacedName
			newConfig.ReleaseVersion = "v1.1.0"

			// Update node to new version to match new config
			node := &corev1.Node{}
			err = fakeClient.Get(ctx, types.NamespacedName{Name: "node1"}, node)
			Expect(err).NotTo(HaveOccurred())
			node.Status.NodeInfo.KubeletVersion = "v1.31.0"
			Expect(fakeClient.Status().Update(ctx, node)).To(Succeed())

			_, err = handler.GenerateSnapshot(ctx, newConfig)
			Expect(err).NotTo(HaveOccurred())

			// Verify old snapshot was replaced
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      testSnapshotName,
				Namespace: config.ReleaseNamespacedName.Namespace,
			}, cm)
			Expect(err).NotTo(HaveOccurred())
			Expect(cm.Annotations["lifecycle.suse.com/source-kubernetes-version"]).To(Equal("v1.31.0"))
		})
	})

	Describe("ReconcileAvailability", func() {
		var snapshot *reconcilers.PackagedComponentsSnapshot

		BeforeEach(func() {
			var err error
			snapshot, err = handler.GenerateSnapshot(ctx, config)
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return available when version matches and no components changed", func() {
			status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

			Expect(err).NotTo(HaveOccurred())
			Expect(status.State).To(Equal(lifecyclev1alpha1.K8sPackagedComponentsAvailable))
			Expect(status.Message).To(Equal("All RKE2 packaged components available"))
		})

		Context("when components changed", func() {
			var chart *helmv1.HelmChart

			BeforeEach(func() {
				// Create initial snapshot with a component
				chart = testutil.NewTestHelmChartCR("rke2-coredns", "kube-system", "1.0.0")
				chart.Annotations = map[string]string{
					testChartURL:  testExampleChart,
					testOwnerName: testRKE2CoreDNS,
					testOwnerGVK:  testAddonGVK,
				}
				chart.Spec.ChartContent = "original-content"
				Expect(fakeClient.Create(ctx, chart)).To(Succeed())

				//nolint:unparam
				findCompsFn = func(_ context.Context, _ client.Client) (map[string]helmv1.HelmChart, error) {
					return map[string]helmv1.HelmChart{testRKE2CoreDNS: *chart}, nil
				}
				handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)

				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return &helm.ReleaseInfo{
						ChartVersion: chart.Spec.Version,
						Namespace:    chart.Namespace,
						Config:       map[string]any{},
						Revisions:    1,
					}, nil
				}

				config.ReleaseVersion = "v1.1.0"
				var err error
				snapshot, err = handler.GenerateSnapshot(ctx, config)
				Expect(err).NotTo(HaveOccurred())
			})

			It("should wait for changed components to complete", func() {
				// Change the chart content
				chart.Spec.ChartContent = testNewContent
				Expect(fakeClient.Update(ctx, chart)).To(Succeed())

				status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})

			It("should return available when job completes", func() {
				// Change the chart content
				chart.Spec.ChartContent = testNewContent
				chart.Status.JobName = testRKE2CoreDNSJob
				Expect(fakeClient.Update(ctx, chart)).To(Succeed())

				// Create completed job
				job := testutil.NewTestJobWithCompletionTime("rke2-coredns-job", "kube-system", time.Now())
				Expect(fakeClient.Create(ctx, job)).To(Succeed())

				status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.K8sPackagedComponentsAvailable))
			})

			Describe("job completion verification", func() {
				BeforeEach(func() {
					findCompsFn = func(ctx context.Context, c client.Client) (map[string]helmv1.HelmChart, error) {
						// Get the chart from the fake client to pick up any updates
						currentChart := &helmv1.HelmChart{}
						err := c.Get(ctx, types.NamespacedName{Name: "rke2-coredns", Namespace: "kube-system"}, currentChart)
						if err != nil {
							return nil, err
						}
						return map[string]helmv1.HelmChart{"rke2-coredns": *currentChart}, nil
					}
					handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)
				})

				It("should detect job completion after snapshot creation", func() {
					// Modify chart
					chart.Spec.ChartContent = testNewContent
					chart.Status.JobName = testRKE2CoreDNSJob
					Expect(fakeClient.Update(ctx, chart)).To(Succeed())

					// Create job that completed after snapshot
					job := testutil.NewTestJobWithCompletionTime("rke2-coredns-job", "kube-system",
						snapshot.CreationTime.Add(1*time.Minute))
					Expect(fakeClient.Create(ctx, job)).To(Succeed())

					status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.K8sPackagedComponentsAvailable))
				})

				It("should not consider job complete if it finished before snapshot", func() {
					// Modify chart
					chart.Spec.ChartContent = testNewContent
					chart.Status.JobName = testRKE2CoreDNSJob
					Expect(fakeClient.Update(ctx, chart)).To(Succeed())

					// Create job that completed before snapshot
					job := testutil.NewTestJobWithCompletionTime("rke2-coredns-job", "kube-system",
						snapshot.CreationTime.Add(-1*time.Minute))
					Expect(fakeClient.Create(ctx, job)).To(Succeed())

					status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				})

				It("should fallback to release check when job not found", func() {
					// Modify chart
					chart.Spec.ChartContent = testNewContent
					chart.Status.JobName = testRKE2CoreDNSJob // Job name is set but job doesn't exist
					Expect(fakeClient.Update(ctx, chart)).To(Succeed())

					// Mock release with version change (indicating upgrade happened)
					mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
						return &helm.ReleaseInfo{
							ChartVersion: "2.0.0", // Changed version
							Namespace:    "kube-system",
							Config:       map[string]any{},
							Revisions:    2, // Increased revisions
						}, nil
					}

					status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.K8sPackagedComponentsAvailable))
				})

				It("should wait when job name is missing", func() {
					// Modify chart but no job name set
					chart.Spec.ChartContent = testNewContent
					Expect(fakeClient.Update(ctx, chart)).To(Succeed())

					status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

					Expect(err).NotTo(HaveOccurred())
					Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				})
			})
		})

		Context("with new components", func() {
			var chart *helmv1.HelmChart

			BeforeEach(func() {
				chart = testutil.NewTestHelmChartCR("rke2-metrics-server", "kube-system", "1.0.0")
				chart.Annotations = map[string]string{
					testChartURL:  testExampleChart,
					testOwnerName: "rke2-metrics-server",
					testOwnerGVK:  testAddonGVK,
				}
				Expect(fakeClient.Create(ctx, chart)).To(Succeed())

				//nolint:unparam
				findCompsFn = func(_ context.Context, _ client.Client) (map[string]helmv1.HelmChart, error) {
					return map[string]helmv1.HelmChart{"rke2-metrics-server": *chart}, nil
				}
				handler = reconcilers.NewRKE2PackagedComponentsHandler(fakeClient, mockHelm, findCompsFn)

				mockHelm.RetrieveReleaseFn = func(name string) (*helm.ReleaseInfo, error) {
					return nil, helm.ErrReleaseNotFound
				}
			})

			It("should wait for new components to be installed", func() {
				status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})

			It("should return available when job completes", func() {
				chart.Status.JobName = "rke2-metrics-job"
				Expect(fakeClient.Update(ctx, chart)).To(Succeed())

				// Create completion job
				job := testutil.NewTestJobWithCompletionTime("rke2-metrics-job", "kube-system", time.Now())
				Expect(fakeClient.Create(ctx, job)).To(Succeed())

				status, err := handler.ReconcileAvailability(ctx, "v1.30.0", snapshot)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.K8sPackagedComponentsAvailable))
			})
		})
	})
})
