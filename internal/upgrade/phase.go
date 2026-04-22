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

package upgrade

import (
	"fmt"
	"strings"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
)

// Phase represents a distinct upgrade phase.
type Phase string

// Phase constants derived from condition types.
var (
	PhaseOS         = Phase(strings.TrimSuffix(lifecyclev1alpha1.ConditionOSUpgraded, "Upgraded"))
	PhaseKubernetes = Phase(strings.TrimSuffix(lifecyclev1alpha1.ConditionKubernetesUpgraded, "Upgraded"))
	PhaseHelmCharts = Phase(strings.TrimSuffix(lifecyclev1alpha1.ConditionHelmChartsUpgraded, "Upgraded"))
)

// ConditionType returns the condition type string for this phase.
func (p Phase) ConditionType() string {
	return string(p) + "Upgraded"
}

// SkippedStatus returns a skipped status for the given phase.
func (p Phase) SkippedStatus() *PhaseStatus {
	return &PhaseStatus{
		State:   lifecyclev1alpha1.UpgradeSkipped,
		Message: fmt.Sprintf("Upgrade for phase '%s' skipped", p),
	}
}

// PhaseStatus contains the status and details for an upgrade phase.
type PhaseStatus struct {
	State   string
	Message string
}

// PhaseError represents an error that occurred during a specific upgrade phase.
// The underlying Err should contain context-specific details from the reconciler.
type PhaseError struct {
	Phase Phase
	Err   error
}

func (e *PhaseError) Error() string {
	return fmt.Sprintf("%s upgrade failed: %v", e.Phase, e.Err)
}

// Result contains the outcome of the upgrade reconciliation.
type Result struct {
	// PhaseStates maps each phase to its current state.
	PhaseStates map[Phase]*PhaseStatus
}

// AllComplete returns true if all phases have either succeeded or been skipped.
func (r *Result) AllComplete() bool {
	for _, state := range r.PhaseStates {
		if state.State != lifecyclev1alpha1.UpgradeSucceeded && state.State != lifecyclev1alpha1.UpgradeSkipped {
			return false
		}
	}
	return true
}
