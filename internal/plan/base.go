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
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/intstr"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
)

const (
	// Namespace for the SUC Plans.
	Namespace = "cattle-system"

	// ControlPlaneLabel is an identifier of control plane nodes.
	ControlPlaneLabel = "node-role.kubernetes.io/control-plane"
)

func basePlan(name string, drain bool) *upgradecattlev1.Plan {
	const (
		kind               = "Plan"
		apiVersion         = "upgrade.cattle.io/v1"
		serviceAccountName = "system-upgrade-controller"
	)

	plan := &upgradecattlev1.Plan{
		TypeMeta: metav1.TypeMeta{
			Kind:       kind,
			APIVersion: apiVersion,
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: Namespace,
		},
		Spec: upgradecattlev1.PlanSpec{
			ServiceAccountName: serviceAccountName,
		},
	}

	if drain {
		timeout := intstr.FromString("15m")
		deleteEmptyDirData := true
		ignoreDaemonSets := true
		plan.Spec.Drain = &upgradecattlev1.DrainSpec{
			Timeout:            &timeout,
			DeleteEmptydirData: &deleteEmptyDirData,
			IgnoreDaemonSets:   &ignoreDaemonSets,
			Force:              true,
		}
	} else {
		// By default when drain is enabled, cordon is enabled as well,
		// so if drain is disabled we need to ensure the node is cordoned.
		plan.Spec.Cordon = true
	}

	return plan
}
