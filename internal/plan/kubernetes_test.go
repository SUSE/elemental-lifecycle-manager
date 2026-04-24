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

package plan

import (
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/utils/ptr"
)

var _ = Describe("Kubernetes plan tests", func() {
	const (
		releaseName    = "test-release"
		releaseVersion = "0.6.0"
		k8sVersion     = "1.35.0"
		drain          = true
	)

	Describe("kubernetesControlPlaneName", func() {
		It("generates correct name with sanitized version", func() {
			name := kubernetesControlPlaneName("1.35.0")
			Expect(name).To(Equal("elemental-kubernetes-control-plane-1-35-0"))
		})

		It("handles version without dots", func() {
			name := kubernetesControlPlaneName("v1")
			Expect(name).To(Equal("elemental-kubernetes-control-plane-v1"))
		})
	})

	Describe("kubernetesWorkerName", func() {
		It("generates correct name with sanitized version", func() {
			name := kubernetesWorkerName("1.35.0")
			Expect(name).To(Equal("elemental-kubernetes-worker-1-35-0"))
		})

		It("handles version without dots", func() {
			name := kubernetesWorkerName("v1")
			Expect(name).To(Equal("elemental-kubernetes-worker-v1"))
		})
	})

	Describe("KubernetesControlPlane", Ordered, func() {
		var plan *upgradecattlev1.Plan

		BeforeAll(func() {
			plan = KubernetesControlPlane(releaseName, releaseVersion, k8sVersion, drain)
		})

		It("creates a plan with correct metadata", func() {
			Expect(plan).ToNot(BeNil())
			Expect(plan.TypeMeta.Kind).To(Equal("Plan"))
			Expect(plan.TypeMeta.APIVersion).To(Equal("upgrade.cattle.io/v1"))
			Expect(plan.ObjectMeta.Name).To(Equal("elemental-kubernetes-control-plane-1-35-0"))
			Expect(plan.ObjectMeta.Namespace).To(Equal(Namespace))
		})

		It("sets release labels", func() {
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseNameLabel, releaseName))
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseVersionLabel, "0-6-0"))
		})

		It("sets correct spec version", func() {
			Expect(plan.Spec.Version).To(Equal(k8sVersion))
		})

		It("sets concurrency to 1", func() {
			Expect(plan.Spec.Concurrency).To(Equal(int64(1)))
		})

		It("selects control plane nodes", func() {
			Expect(plan.Spec.NodeSelector).ToNot(BeNil())
			Expect(plan.Spec.NodeSelector.MatchExpressions).To(HaveLen(1))

			expr := plan.Spec.NodeSelector.MatchExpressions[0]
			Expect(expr.Key).To(Equal("node-role.kubernetes.io/control-plane"))
			Expect(expr.Operator).To(Equal(metav1.LabelSelectorOpExists))
		})

		It("configures upgrade container with base image", func() {
			Expect(plan.Spec.Upgrade).ToNot(BeNil())
			Expect(plan.Spec.Upgrade.Image).To(Equal(rke2UpgradeImage))
		})

		It("enables drain with correct settings", func() {
			Expect(plan.Spec.Drain).ToNot(BeNil())
			Expect(ptr.Deref(plan.Spec.Drain.DeleteEmptydirData, false)).To(BeTrue())
			Expect(ptr.Deref(plan.Spec.Drain.IgnoreDaemonSets, false)).To(BeTrue())
			Expect(plan.Spec.Drain.Force).To(BeTrue())
			Expect(plan.Spec.Drain.Timeout.String()).To(Equal("15m"))
		})
	})

	Describe("KubernetesWorker", Ordered, func() {
		var plan *upgradecattlev1.Plan

		BeforeAll(func() {
			plan = KubernetesWorker(releaseName, releaseVersion, k8sVersion, drain)
		})

		It("creates a plan with correct metadata", func() {
			Expect(plan).ToNot(BeNil())
			Expect(plan.TypeMeta.Kind).To(Equal("Plan"))
			Expect(plan.TypeMeta.APIVersion).To(Equal("upgrade.cattle.io/v1"))
			Expect(plan.ObjectMeta.Name).To(Equal("elemental-kubernetes-worker-1-35-0"))
			Expect(plan.ObjectMeta.Namespace).To(Equal(Namespace))
		})

		It("sets release labels", func() {
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseNameLabel, releaseName))
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseVersionLabel, "0-6-0"))
		})

		It("sets correct spec version", func() {
			Expect(plan.Spec.Version).To(Equal(k8sVersion))
		})

		It("sets concurrency to 1", func() {
			Expect(plan.Spec.Concurrency).To(Equal(int64(1)))
		})

		It("selects worker nodes (not control plane)", func() {
			Expect(plan.Spec.NodeSelector).ToNot(BeNil())
			Expect(plan.Spec.NodeSelector.MatchExpressions).To(HaveLen(1))

			expr := plan.Spec.NodeSelector.MatchExpressions[0]
			Expect(expr.Key).To(Equal("node-role.kubernetes.io/control-plane"))
			Expect(expr.Operator).To(Equal(metav1.LabelSelectorOpDoesNotExist))
		})

		It("enables drain with correct settings", func() {
			Expect(plan.Spec.Drain).ToNot(BeNil())
			Expect(ptr.Deref(plan.Spec.Drain.DeleteEmptydirData, false)).To(BeTrue())
			Expect(ptr.Deref(plan.Spec.Drain.IgnoreDaemonSets, false)).To(BeTrue())
			Expect(plan.Spec.Drain.Force).To(BeTrue())
			Expect(plan.Spec.Drain.Timeout.String()).To(Equal("15m"))
		})
	})
})
