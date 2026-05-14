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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	"github.com/rancher/wrangler/v3/pkg/genericcondition"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers/testutil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

const (
	testLabelKey    = "test"
	testLabelNode   = "node"
	testLabelRole   = "role"
	testLabelWorker = "worker"
)

var _ = Describe("SUCPlanReconciler", func() {
	var (
		ctx        context.Context
		reconciler *reconcilers.SUCPlanReconciler
		fakeClient client.Client
		scheme     *runtime.Scheme
	)

	BeforeEach(func() {
		ctx = context.Background()
		scheme = testutil.NewTestScheme()
		fakeClient = testutil.NewFakeClient(scheme)
		reconciler = reconcilers.NewSUCPlanReconciler(fakeClient)
	})

	Describe("Reconcile", func() {
		var desiredPlan *upgradecattlev1.Plan

		BeforeEach(func() {
			desiredPlan = testutil.NewTestSUCPlan("test-plan", "default")
		})

		It("should create the plan and return in-progress status", func() {
			desiredPlan.Spec.NodeSelector.MatchLabels["key"] = "value"
			result, err := reconciler.Reconcile(ctx, desiredPlan)

			Expect(err).NotTo(HaveOccurred())
			Expect(result).NotTo(BeNil())
			Expect(result.Status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			Expect(result.Status.Message).To(ContainSubstring("test-plan"))
			Expect(result.Nodes).To(BeEmpty())

			// Verify plan was created in the cluster
			created := &upgradecattlev1.Plan{}
			err = fakeClient.Get(ctx, types.NamespacedName{
				Name:      desiredPlan.Name,
				Namespace: desiredPlan.Namespace,
			}, created)
			Expect(err).NotTo(HaveOccurred())
			Expect(created.Spec).To(Equal(desiredPlan.Spec))
		})

		It("should return in-progress status with applying nodes empty and then getting populated", func() {
			Expect(fakeClient.Create(ctx, desiredPlan)).To(Succeed())
			desiredPlan.Status.Applying = []string{}
			Expect(fakeClient.Update(ctx, desiredPlan)).To(Succeed())

			result, err := reconciler.Reconcile(ctx, desiredPlan)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
			Expect(result.Status.Message).To(ContainSubstring("execution in progress"))

			// now populate the Applying field in Status
			desiredPlan.Status.Applying = []string{"node1", "node2"}
			Expect(fakeClient.Update(ctx, desiredPlan)).To(Succeed())

			result, err = reconciler.Reconcile(ctx, desiredPlan)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))

			Expect(result.Status.Message).To(SatisfyAll(
				ContainSubstring("applying on"),
				ContainSubstring("node1"),
				ContainSubstring("node2"),
			))
			Expect(result.Nodes).To(BeEmpty())
		})

		It("should return in-progress status", func() {
			desiredPlan.Status.Conditions = []genericcondition.GenericCondition{
				{
					Type:   string(upgradecattlev1.PlanComplete),
					Status: corev1.ConditionFalse,
					Reason: "",
				},
			}
			Expect(fakeClient.Create(ctx, desiredPlan)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, desiredPlan)
			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status.State).To(Equal(lifecyclev1alpha1.UpgradeInProgress))
		})

		It("should return failed status with message", func() {
			desiredPlan.Status.Conditions = []genericcondition.GenericCondition{
				{
					Type:    string(upgradecattlev1.PlanComplete),
					Status:  corev1.ConditionFalse,
					Reason:  "UpgradeFailed",
					Message: "upgrade script exited with code 1",
				},
			}
			Expect(fakeClient.Create(ctx, desiredPlan)).To(Succeed())
			result, err := reconciler.Reconcile(ctx, desiredPlan)

			Expect(err).NotTo(HaveOccurred())
			Expect(result.Status.State).To(Equal(lifecyclev1alpha1.UpgradeFailed))
			Expect(result.Status.Message).To(ContainSubstring("failed"))
			Expect(result.Status.Message).To(ContainSubstring("upgrade script exited with code 1"))
		})

		Context("when plan is complete", func() {
			BeforeEach(func() {
				desiredPlan.Status.Conditions = []genericcondition.GenericCondition{
					{
						Type:   string(upgradecattlev1.PlanComplete),
						Status: corev1.ConditionTrue,
					},
				}
				desiredPlan.Spec.NodeSelector = &metav1.LabelSelector{
					MatchLabels: map[string]string{testLabelKey: testLabelNode},
				}
				Expect(fakeClient.Create(ctx, desiredPlan)).To(Succeed())
			})

			It("should return complete status when no nodes match", func() {
				result, err := reconciler.Reconcile(ctx, desiredPlan)

				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status.State).To(Equal(lifecyclev1alpha1.PlanComplete))
				Expect(result.Status.Message).To(Equal("Plan test-plan execution completed successfully"))
				Expect(result.Nodes).To(BeEmpty())
			})

			It("should list nodes matching the plan selector", func() {
				// Create matching nodes
				node1 := testutil.NewTestNode("node1", false)
				node1.Labels = map[string]string{testLabelKey: testLabelNode}
				Expect(fakeClient.Create(ctx, node1)).To(Succeed())

				node2 := testutil.NewTestNode("node2", false)
				node2.Labels = map[string]string{testLabelKey: testLabelNode}
				Expect(fakeClient.Create(ctx, node2)).To(Succeed())

				// Create non-matching node
				node3 := testutil.NewTestNode("node3", false)
				node3.Labels = map[string]string{"different": "label"}
				Expect(fakeClient.Create(ctx, node3)).To(Succeed())

				result, err := reconciler.Reconcile(ctx, desiredPlan)

				Expect(err).NotTo(HaveOccurred())
				Expect(result.Status.State).To(Equal(lifecyclev1alpha1.PlanComplete))
				Expect(result.Nodes).To(HaveLen(2))

				nodeNames := []string{result.Nodes[0].Name, result.Nodes[1].Name}
				Expect(nodeNames).To(ConsistOf("node1", "node2"))
			})
		})
	})
})
