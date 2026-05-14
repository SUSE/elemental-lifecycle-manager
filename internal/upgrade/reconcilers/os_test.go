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
	testPlanCompleted = "Plan completed"
	testComplete      = "Complete"
)

var _ = Describe("OSReconciler", func() {
	var (
		ctx              context.Context
		reconciler       *reconcilers.OSReconciler
		fakeClient       client.Client
		mockPlanRec      *testutil.MockPlanReconciler
		scheme           *runtime.Scheme
		config           *upgrade.Config
		planReconcileCnt int
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = testutil.NewTestScheme()
		fakeClient = testutil.NewFakeClient(scheme)
		mockPlanRec = testutil.NewMockPlanReconciler()
		reconciler = reconcilers.NewOSReconciler(fakeClient, mockPlanRec)
		planReconcileCnt = 0

		config = testutil.NewTestConfig(testutil.WithOS("registry.example.com/os:v1.0.0", "v1.0.0"))
	})

	Describe("Phase", func() {
		It("should return PhaseOS", func() {
			Expect(reconciler.Phase()).To(Equal(upgrade.PhaseOS))
		})
	})

	Describe("Reconcile", func() {
		Context("when OS config is nil", func() {
			It("should skip the phase", func() {
				config.OS = nil

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSkipped))
				Expect(status.Message).To(ContainSubstring("OS"))
			})
		})

		Context("with control-plane only cluster", func() {
			BeforeEach(func() {
				// Create only control-plane nodes
				node1 := testutil.NewTestNode("cp1", true)
				Expect(fakeClient.Create(ctx, node1)).To(Succeed())

				node2 := testutil.NewTestNode("cp2", true)
				Expect(fakeClient.Create(ctx, node2)).To(Succeed())

				// Mock plan reconciler to return complete status
				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					planReconcileCnt++
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testPlanCompleted,
						},
						Nodes: []corev1.Node{},
					}, nil
				}
			})

			It("should create only control-plane plan and succeed", func() {
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status).NotTo(BeNil())
				Expect(planReconcileCnt).To(Equal(1)) // Only control-plane plan
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
				Expect(status.Message).To(Equal("All nodes upgraded successfully"))
			})
		})

		Context("with control-plane and worker nodes", func() {
			BeforeEach(func() {
				// Create control-plane nodes
				cp1 := testutil.NewTestNode("cp1", true)
				Expect(fakeClient.Create(ctx, cp1)).To(Succeed())

				// Create worker nodes
				worker1 := testutil.NewTestNode("worker1", false)
				Expect(fakeClient.Create(ctx, worker1)).To(Succeed())

				worker2 := testutil.NewTestNode("worker2", false)
				Expect(fakeClient.Create(ctx, worker2)).To(Succeed())
			})

			It("should wait for control-plane plan before starting worker plan", func() {
				callCount := 0
				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					callCount++
					if callCount == 1 {
						// First call (control-plane) returns in-progress
						return &reconcilers.PlanResult{
							Status: &upgrade.PhaseStatus{
								State:   lifecyclev1alpha1.UpgradeInProgress,
								Message: "Control-plane upgrade in progress",
							},
							Nodes: []corev1.Node{},
						}, nil
					}
					// Should not reach here in this test
					Fail("Worker plan should not be reconciled while control-plane is in progress")
					return nil, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(status.Message).To(ContainSubstring("Control-plane"))
				Expect(callCount).To(Equal(1)) // Only control-plane plan should be called
			})

			It("should proceed to worker plan after control-plane completes", func() {
				callCount := 0
				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					callCount++
					if callCount == 1 {
						// Control-plane plan completes
						return &reconcilers.PlanResult{
							Status: &upgrade.PhaseStatus{
								State:   lifecyclev1alpha1.PlanComplete,
								Message: "Control-plane complete",
							},
							Nodes: []corev1.Node{},
						}, nil
					}
					// Worker plan in progress
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.UpgradeInProgress,
							Message: "Worker upgrade in progress",
						},
						Nodes: []corev1.Node{},
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
				Expect(status.Message).To(ContainSubstring("Worker"))
				Expect(callCount).To(Equal(2))
			})

			It("should return succeeded when all plans complete", func() {
				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.PlanComplete,
							Message: testPlanCompleted,
						},
						Nodes: []corev1.Node{},
					}, nil
				}

				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeSucceeded))
				Expect(status.Message).To(ContainSubstring("All nodes upgraded successfully"))
			})
		})

		Context("when plan reconciliation fails", func() {
			BeforeEach(func() {
				node := testutil.NewTestNode("node1", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return nil, fmt.Errorf("plan reconciliation failed")
				}
			})

			It("should return the error", func() {
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).To(HaveOccurred())
				Expect(err.Error()).To(ContainSubstring("plan reconciliation failed"))
				Expect(status).To(BeNil())
			})
		})

		Context("when plan returns failed status", func() {
			BeforeEach(func() {
				node := testutil.NewTestNode("node1", true)
				Expect(fakeClient.Create(ctx, node)).To(Succeed())

				mockPlanRec.ReconcileFn = func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
					return &reconcilers.PlanResult{
						Status: &upgrade.PhaseStatus{
							State:   lifecyclev1alpha1.UpgradeFailed,
							Message: "Upgrade script failed",
						},
						Nodes: []corev1.Node{},
					}, nil
				}
			})

			It("should return the failed status", func() {
				status, err := reconciler.Reconcile(ctx, config)

				Expect(err).NotTo(HaveOccurred())
				Expect(status.State).To(Equal(lifecyclev1alpha1.UpgradeFailed))
				Expect(status.Message).To(ContainSubstring("Upgrade script failed"))
			})
		})
	})
})
