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
	"time"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testK8sVersion          = "v1.30.0"
	testCompleteMsg         = "Complete"
	testComponentsAvailable = "Components available"
)

var _ = Describe("KubernetesReconciler", func() {
	var (
		ctx                 context.Context
		reconciler          *reconcilers.KubernetesReconciler
		fakeClient          client.Client
		mockPlanRec         *testutil.MockPlanReconciler
		mockPackagedHandler *testutil.MockPackagedComponentsHandler
		scheme              *runtime.Scheme
		config              *upgrade.Config
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = testutil.NewTestScheme()
		fakeClient = testutil.NewFakeClient(scheme)
		mockPlanRec = testutil.NewMockPlanReconciler()
		mockPackagedHandler = testutil.NewMockPackagedComponentsHandler()
		reconciler = reconcilers.NewKubernetesReconciler(fakeClient, mockPlanRec, mockPackagedHandler)

		config = testutil.NewTestConfigWithKubernetes("registry.example.com/k8s:v1.30.0", "v1.30.0")
	})

	Describe("Phase", func() {
		It("should return PhaseKubernetes", func() {
			Expect(reconciler.Phase()).To(Equal(upgrade.PhaseKubernetes))
		})
	})

	Describe("Reconcile", func() {
		Context("when Kubernetes config is nil", func() {
			It("should skip the phase", func() {
				config.Kubernetes = nil

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
				Expect(status.Message).To(ContainSubstring("Kubernetes"))
			})
		})

		Context("when config is nil", func() {
			It("should skip the phase", func() {
				status, err := reconciler.Reconcile(ctx, nil)

				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
			})
		})

		Context("snapshot generation", func() {
			BeforeEach(func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())
			})

			It("should generate snapshot before upgrading", func() {
				snapshotGenerated := false
				mockPackagedHandler.GenerateSnapshotFn = func(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error) {
					snapshotGenerated = true
					return &reconcilers.PackagedComponentsSnapshot{
						CreationTime:            time.Now(),
						SourceKubernetesVersion: testK8sVersion,
						Charts:                  []*reconcilers.PackagedComponentChartSnapshot{},
					}, nil
				}

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)},
					}, nil
				}

				mockPackagedHandler.ReconcileAvailabilityFn = func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
					return &upgrade.PhaseStatus{
						State:   lifecyclev1alpha1.K8sPackagedComponentsAvailable,
						Message: testComponentsAvailable,
					}, nil
				}

				_, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(snapshotGenerated).To(BeTrue())
			})

			It("should return error if snapshot generation fails", func() {
				mockPackagedHandler.GenerateSnapshotFn = func(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error) {
					return nil, fmt.Errorf("snapshot generation failed")
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("snapshot generation failed"))
				Expect(status).To(BeNil())
			})
		})

		Context("plan execution", func() {
			var planNames []string

			BeforeEach(func() {
				planNames = []string{}

				mockPackagedHandler.GenerateSnapshotFn = func(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error) {
					return &reconcilers.PackagedComponentsSnapshot{
						CreationTime:            time.Now(),
						SourceKubernetesVersion: testK8sVersion,
						Charts:                  []*reconcilers.PackagedComponentChartSnapshot{},
					}, nil
				}

				mockPackagedHandler.ReconcileAvailabilityFn = func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
					return &upgrade.PhaseStatus{
						State:   lifecyclev1alpha1.K8sPackagedComponentsAvailable,
						Message: testComponentsAvailable,
					}, nil
				}
			})

			It("should execute control-plane plan first", func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					planNames = append(planNames, desired.Name)
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*node},
					}, nil
				}

				_, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(planNames).To(HaveLen(1))
				Expect(planNames[0]).To(ContainSubstring("control-plane"))
			})

			It("should execute control-plane then worker plans in order", func() {
				cp := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				Expect(fakeClient.Create(ctx, cp)).To(Succeed())

				worker := testutil.NewTestNodeWithVersion("worker1", "v1.30.0", false)
				Expect(fakeClient.Create(ctx, worker)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					planNames = append(planNames, desired.Name)
					var nodes []corev1.Node
					if len(planNames) == 1 {
						nodes = []corev1.Node{*cp}
					} else {
						nodes = []corev1.Node{*worker}
					}
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: nodes,
					}, nil
				}

				_, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(planNames).To(HaveLen(2))
				Expect(planNames[0]).To(ContainSubstring("control-plane"))
				Expect(planNames[1]).To(ContainSubstring("worker"))
			})

			It("should wait for control-plane before starting worker", func() {
				cp := testutil.NewTestNode("cp1", true)
				Expect(fakeClient.Create(ctx, cp)).To(Succeed())

				worker := testutil.NewTestNode("worker1", false)
				Expect(fakeClient.Create(ctx, worker)).To(Succeed())

				callCount := 0
				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					callCount++
					if callCount == 1 {
						return &reconcilers.PlanResult{
							Status: &upgrade.PhaseStatus{
								State:   lifecyclev1alpha1.UpgradeInProgress,
								Message: "Control-plane in progress",
							},
							Nodes: []corev1.Node{},
						}, nil
					}
					Fail("Worker plan should not be called")
					return nil, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(callCount).To(Equal(1))
			})
		})

		Context("node version verification", func() {
			BeforeEach(func() {
				mockPackagedHandler.GenerateSnapshotFn = func(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error) {
					return &reconcilers.PackagedComponentsSnapshot{
						CreationTime:            time.Now(),
						SourceKubernetesVersion: testK8sVersion,
						Charts:                  []*reconcilers.PackagedComponentChartSnapshot{},
					}, nil
				}
			})

			It("should verify all nodes are at target version", func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*node},
					}, nil
				}

				mockPackagedHandler.ReconcileAvailabilityFn = func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
					return &upgrade.PhaseStatus{
						State:   lifecyclev1alpha1.K8sPackagedComponentsAvailable,
						Message: testComponentsAvailable,
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
			})

			It("should return in-progress when node version doesn't match", func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.29.0", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*node},
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(status.Message).To(ContainSubstring("waiting for node upgrade verification"))
			})

			It("should return in-progress when node is not ready", func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				// Make node not ready
				node.Status.Conditions = []corev1.NodeCondition{
					{
						Type:   corev1.NodeReady,
						Status: corev1.ConditionFalse,
					},
				}
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*node},
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})

			It("should return in-progress when node is unschedulable", func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				node.Spec.Unschedulable = true
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*node},
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			})
		})

		Context("packaged components availability", func() {
			BeforeEach(func() {
				node := testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPackagedHandler.GenerateSnapshotFn = func(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error) {
					return &reconcilers.PackagedComponentsSnapshot{
						CreationTime:            time.Now(),
						SourceKubernetesVersion: testK8sVersion,
						Charts:                  []*reconcilers.PackagedComponentChartSnapshot{},
					}, nil
				}

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testCompleteMsg,
						},
						Nodes: []corev1.Node{*testutil.NewTestNodeWithVersion("cp1", "v1.30.0", true)},
					}, nil
				}
			})

			It("should wait for packaged components to become available", func() {
				mockPackagedHandler.ReconcileAvailabilityFn = func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
					return &upgrade.PhaseStatus{
						State:   lifecyclev1alpha1.UpgradeInProgress,
						Message: "Waiting for components",
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(status.Message).To(ContainSubstring("Waiting for components"))
			})

			It("should succeed when components are available", func() {
				mockPackagedHandler.ReconcileAvailabilityFn = func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
					return &upgrade.PhaseStatus{
						State:   lifecyclev1alpha1.K8sPackagedComponentsAvailable,
						Message: testComponentsAvailable,
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
				Expect(status.Message).To(ContainSubstring("successfully"))
			})

			It("should return error if availability check fails", func() {
				mockPackagedHandler.ReconcileAvailabilityFn = func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
					return nil, fmt.Errorf("availability check failed")
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("availability check failed"))
				Expect(status).To(BeNil())
			})
		})
	})

})
