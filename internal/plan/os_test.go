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

var _ = Describe("OS plan tests", func() {
	const (
		releaseName           = "test-release"
		releaseVersion        = "0.6.0"
		osImage               = "registry.example.com/elemental-os:1.2.3"
		osVersion             = "1.2.3"
		drain                 = true
		expectedUpgradeScript = `set -e

HOST="${HOST:-/host}"
DEPLOYMENT="${DEPLOYMENT:-$HOST/etc/elemental/deployment.yaml}"
OS_IMAGE_REPO="registry.example.com/elemental-os"
OS_VERSION="1.2.3"
INCOMING="$OS_IMAGE_REPO:$OS_VERSION"
CURRENT=$(grep -F "uri: oci://$OS_IMAGE_REPO" "$DEPLOYMENT" 2>/dev/null || true)

# On fresh systems, we have a sourceOS specified with raw (e.g. raw://../squashfs.img) data
# instead of from an OCI image, so for instances that CURRENT is empty we
# assume that this is a fresh system and proceed with the upgrade.
if [ -n "$CURRENT" ]; then
	# Extract the prefix (e.g. "uri: oci://") before the OS_IMAGE_REPO,
	# so that it can be stripped in the next step.
    prefix=${CURRENT%%"$OS_IMAGE_REPO"*}
	CURRENT=${CURRENT#"$prefix"}
    if [ "$CURRENT" = "$INCOMING" ]; then
        echo "Active OS image is already at correct version $OS_VERSION. Upgrade has been performed."
        exit 0
    fi
fi

USE_LOCAL_IMAGES=false upgrader "$INCOMING" && chroot /host reboot
`
	)

	Describe("osControlPlaneName", func() {
		It("generates correct name with sanitized version", func() {
			name := osControlPlaneName("1.2.3")
			Expect(name).To(Equal("elemental-os-control-plane-1-2-3"))
		})

		It("handles version without dots", func() {
			name := osControlPlaneName("v1")
			Expect(name).To(Equal("elemental-os-control-plane-v1"))
		})
	})

	Describe("osWorkerName", func() {
		It("generates correct name with sanitized version", func() {
			name := osWorkerName("1.2.3")
			Expect(name).To(Equal("elemental-os-worker-1-2-3"))
		})

		It("handles version without dots", func() {
			name := osWorkerName("v1")
			Expect(name).To(Equal("elemental-os-worker-v1"))
		})
	})

	Describe("OSControlPlane", Ordered, func() {
		var plan *upgradecattlev1.Plan

		BeforeAll(func() {
			var err error
			plan, err = OSControlPlane(releaseName, osImage, osVersion, releaseVersion, drain)
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates a plan with correct metadata", func() {
			Expect(plan).ToNot(BeNil())
			Expect(plan.TypeMeta.Kind).To(Equal("Plan"))
			Expect(plan.TypeMeta.APIVersion).To(Equal("upgrade.cattle.io/v1"))
			Expect(plan.ObjectMeta.Name).To(Equal("elemental-os-control-plane-1-2-3"))
			Expect(plan.ObjectMeta.Namespace).To(Equal(Namespace))
		})

		It("sets release labels", func() {
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseNameLabel, releaseName))
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseVersionLabel, "0-6-0"))
		})

		It("sets correct spec version", func() {
			Expect(plan.Spec.Version).To(Equal(osVersion))
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

		It("configures upgrade container", func() {
			Expect(plan.Spec.Upgrade).ToNot(BeNil())
			Expect(plan.Spec.Upgrade.Image).To(Equal(osImage))
			Expect(plan.Spec.Upgrade.Command).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(plan.Spec.Upgrade.Args).To(Equal([]string{expectedUpgradeScript}))
		})

		It("enables drain with correct settings", func() {
			Expect(plan.Spec.Drain).ToNot(BeNil())
			Expect(ptr.Deref(plan.Spec.Drain.DeleteEmptydirData, false)).To(BeTrue())
			Expect(ptr.Deref(plan.Spec.Drain.IgnoreDaemonSets, false)).To(BeTrue())
			Expect(plan.Spec.Drain.Force).To(BeTrue())
			Expect(plan.Spec.Drain.Timeout.String()).To(Equal("15m"))
		})
	})

	Describe("OSWorker", Ordered, func() {
		var plan *upgradecattlev1.Plan

		BeforeAll(func() {
			var err error
			plan, err = OSWorker(releaseName, osImage, osVersion, releaseVersion, drain)
			Expect(err).ToNot(HaveOccurred())
		})

		It("creates a plan with correct metadata", func() {
			Expect(plan).ToNot(BeNil())
			Expect(plan.TypeMeta.Kind).To(Equal("Plan"))
			Expect(plan.TypeMeta.APIVersion).To(Equal("upgrade.cattle.io/v1"))
			Expect(plan.ObjectMeta.Name).To(Equal("elemental-os-worker-1-2-3"))
			Expect(plan.ObjectMeta.Namespace).To(Equal(Namespace))
		})

		It("sets release labels", func() {
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseNameLabel, releaseName))
			Expect(plan.Labels).To(HaveKeyWithValue(lifecyclev1alpha1.ReleaseVersionLabel, "0-6-0"))
		})

		It("sets correct spec version", func() {
			Expect(plan.Spec.Version).To(Equal(osVersion))
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

		It("configures upgrade container", func() {
			Expect(plan.Spec.Upgrade).ToNot(BeNil())
			Expect(plan.Spec.Upgrade.Image).To(Equal(osImage))
			Expect(plan.Spec.Upgrade.Command).To(Equal([]string{"/bin/sh", "-c"}))
			Expect(plan.Spec.Upgrade.Args).To(Equal([]string{expectedUpgradeScript}))
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
