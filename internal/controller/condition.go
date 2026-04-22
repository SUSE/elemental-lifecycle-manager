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

package controller

import (
	"errors"
	"fmt"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

func setCondition(release *lifecyclev1alpha1.Release, conditionType string, status metav1.ConditionStatus, reason, message string) {
	apimeta.SetStatusCondition(&release.Status.Conditions, metav1.Condition{
		Type:               conditionType,
		Status:             status,
		ObservedGeneration: release.Generation,
		Reason:             reason,
		Message:            message,
	})
}

// initializePendingConditions sets pending conditions for both the manifest and upgrade phases.
func initializePendingConditions(release *lifecyclev1alpha1.Release, phases []upgrade.Phase) {
	existing := apimeta.FindStatusCondition(release.Status.Conditions, lifecyclev1alpha1.ConditionManifestResolved)
	if existing == nil {
		setCondition(release, lifecyclev1alpha1.ConditionManifestResolved, metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradePending, "Waiting for release manifest to be resolved")
	}

	for _, phase := range phases {
		conditionType := phase.ConditionType()
		existing := apimeta.FindStatusCondition(release.Status.Conditions, conditionType)
		if existing == nil {
			setCondition(release, conditionType, metav1.ConditionFalse,
				lifecyclev1alpha1.UpgradePending, "Waiting for previous phases to complete")
		}
	}
}

// setPhaseConditionFromError sets a failed condition based on a PhaseError.
func setPhaseConditionFromError(release *lifecyclev1alpha1.Release, err error) {
	var phaseErr *upgrade.PhaseError
	if errors.As(err, &phaseErr) {
		setCondition(release, phaseErr.Phase.ConditionType(), metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradeFailed, phaseErr.Err.Error())
	}
}

// updatePhaseConditions updates the conditions for all phases from the reconciliation result.
func updatePhaseConditions(release *lifecyclev1alpha1.Release, result *upgrade.Result) {
	for phase, state := range result.PhaseStates {
		updatePhaseCondition(release, phase, state)
	}
}

// updatePhaseCondition updates the condition for a phase based on its observed status.
func updatePhaseCondition(release *lifecyclev1alpha1.Release, phase upgrade.Phase, state *upgrade.PhaseStatus) {
	conditionType := phase.ConditionType()
	conditionStatus := metav1.ConditionFalse

	if state.State == lifecyclev1alpha1.UpgradeSucceeded || state.State == lifecyclev1alpha1.UpgradeSkipped {
		// Only update if not already succeeded or skipped to preserve timestamp
		existing := apimeta.FindStatusCondition(release.Status.Conditions, conditionType)
		if existing != nil && existing.Status == metav1.ConditionTrue {
			return
		}
		conditionStatus = metav1.ConditionTrue
	}

	setCondition(release, conditionType, conditionStatus, state.State, state.Message)
}

// updateAppliedCondition sets the Applied condition based on all phase conditions.
func updateAppliedCondition(release *lifecyclev1alpha1.Release, phases []upgrade.Phase) {
	// Check if manifest is retrieved
	manifestCond := apimeta.FindStatusCondition(release.Status.Conditions, lifecyclev1alpha1.ConditionManifestResolved)
	if manifestCond == nil || manifestCond.Status != metav1.ConditionTrue {
		setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradeFailed, "Manifest not retrieved")
		return
	}

	allSucceeded := true
	var failedPhase, inProgressPhase string

	for _, phase := range phases {
		conditionType := phase.ConditionType()
		cond := apimeta.FindStatusCondition(release.Status.Conditions, conditionType)
		if cond == nil {
			allSucceeded = false
			continue
		}

		if cond.Status != metav1.ConditionTrue {
			allSucceeded = false
			if cond.Reason == lifecyclev1alpha1.UpgradeFailed {
				failedPhase = conditionType
			} else if cond.Reason == lifecyclev1alpha1.UpgradeInProgress && inProgressPhase == "" {
				inProgressPhase = conditionType
			}
		}
	}

	switch {
	case allSucceeded:
		setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionTrue,
			lifecyclev1alpha1.UpgradeSucceeded, "All upgrade phases completed successfully")
	case failedPhase != "":
		setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradeFailed, fmt.Sprintf("Phase %s failed", failedPhase))
	case inProgressPhase != "":
		setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradeInProgress, fmt.Sprintf("Phase %s is in progress", inProgressPhase))
	default:
		setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradePending, "Upgrade pending")
	}
}
