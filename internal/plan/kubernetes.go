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
	"fmt"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	kubernetesControlPlaneBaseName = "elemental-kubernetes-control-plane"
	kubernetesWorkerBaseName       = "elemental-kubernetes-worker"
	rke2UpgradeImage               = "rancher/rke2-upgrade"
)

// kubernetesControlPlaneName returns the full plan name for the given version.
func kubernetesControlPlaneName(version string) string {
	return fmt.Sprintf("%s-%s", kubernetesControlPlaneBaseName, lifecyclev1alpha1.SanitizeVersion(version))
}

// kubernetesWorkerName returns the full plan name for the given version.
func kubernetesWorkerName(version string) string {
	return fmt.Sprintf("%s-%s", kubernetesWorkerBaseName, lifecyclev1alpha1.SanitizeVersion(version))
}

// KubernetesControlPlane builds a SUC Plan for Kubernetes upgrades on control plane nodes.
func KubernetesControlPlane(releaseName, releaseVersion, k8sVersion string, drain bool) *upgradecattlev1.Plan {
	p := basePlan(kubernetesControlPlaneName(k8sVersion), drain)
	p.Labels = map[string]string{
		lifecyclev1alpha1.ReleaseNameLabel:    releaseName,
		lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(releaseVersion),
	}
	p.Spec.Version = k8sVersion
	p.Spec.Concurrency = 1
	p.Spec.NodeSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      ControlPlaneLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	p.Spec.Upgrade = &upgradecattlev1.ContainerSpec{
		Image: rke2UpgradeImage,
	}
	return p
}

// KubernetesWorker builds a SUC Plan for Kubernetes upgrades on worker nodes.
func KubernetesWorker(releaseName, releaseVersion, k8sVersion string, drain bool) *upgradecattlev1.Plan {
	controlPlanePlanName := kubernetesControlPlaneName(k8sVersion)

	p := basePlan(kubernetesWorkerName(k8sVersion), drain)
	p.Labels = map[string]string{
		lifecyclev1alpha1.ReleaseNameLabel:    releaseName,
		lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(releaseVersion),
	}

	p.Spec.Version = k8sVersion
	p.Spec.Concurrency = 1
	p.Spec.NodeSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      ControlPlaneLabel,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			},
		},
	}
	p.Spec.Prepare = &upgradecattlev1.ContainerSpec{
		// Ensures the upgrade for workers will happen only when the
		// control plane SUCPlan has completed.
		Args: []string{
			"prepare",
			controlPlanePlanName,
		},
		Image: rke2UpgradeImage,
	}
	p.Spec.Upgrade = &upgradecattlev1.ContainerSpec{
		Image: rke2UpgradeImage,
	}
	return p
}
