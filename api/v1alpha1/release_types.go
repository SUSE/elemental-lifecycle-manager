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

package v1alpha1

import (
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// Condition types for Release status.
const (
	// ConditionApplied indicates whether the release has been applied successfully.
	// True when all upgrade phase conditions (OS, Kubernetes, HelmCharts) are True.
	ConditionApplied = "Applied"
	// ConditionManifestResolved indicates whether the release manifest was successfully retrieved.
	ConditionManifestResolved = "ManifestResolved"
	// ConditionOSUpgraded indicates the status of the OS upgrade.
	// Pending -> InProgress -> Succeeded/Failed
	ConditionOSUpgraded = "OSUpgraded"
	// ConditionKubernetesUpgraded indicates the status of the Kubernetes upgrade.
	// Pending -> InProgress -> Succeeded/Failed
	ConditionKubernetesUpgraded = "KubernetesUpgraded"
	// ConditionHelmChartsUpgraded indicates the status of Helm chart upgrade.
	// Pending -> InProgress -> Succeeded/Failed
	ConditionHelmChartsUpgraded = "HelmChartsUpgraded"
)

// Condition reasons for Release status.
const (
	// UpgradePending indicates that the upgrade process has not begun.
	UpgradePending = "Pending"

	// UpgradeInProgress indicates that the upgrade process has started.
	UpgradeInProgress = "InProgress"

	// UpgradeSkipped indicates that the upgrade has been skipped.
	UpgradeSkipped = "Skipped"

	// UpgradeSucceeded indicates that the upgrade process has been successful.
	UpgradeSucceeded = "Succeeded"

	// UpgradeFailed indicates that the upgrade process has failed.
	UpgradeFailed = "Failed"
)

// ReleaseSpec defines the desired state of Release
type ReleaseSpec struct {
	// Version specifies the target version of the target release.
	Version string `json:"version"`
	// Registry specifies an OCI registry to fetch release metadata from.
	Registry string `json:"registry"`
	// DisableDrain specifies whether nodes drain should be disabled.
	// +optional
	DisableDrain bool `json:"disableDrain"`
}

// ReleaseStatus defines the observed state of Release
type ReleaseStatus struct {
	// Version is the release version that is currently applied on the environment.
	Version string `json:"version"`
	// Conditions represent the current state of the release upgrade.
	Conditions []metav1.Condition `json:"conditions,omitempty"`
	// ObservedGeneration is the latest generation observed by the controller. Meant for internal use only.
	ObservedGeneration int64 `json:"observedGeneration,omitempty"`
}

// +kubebuilder:object:root=true
// +kubebuilder:subresource:status

// Release is the Schema for the releases API
type Release struct {
	metav1.TypeMeta `json:",inline"`

	// metadata is a standard object metadata
	// +optional
	metav1.ObjectMeta `json:"metadata,omitzero"`

	// spec defines the desired state of Release
	// +required
	Spec ReleaseSpec `json:"spec"`

	// status defines the observed state of Release
	// +optional
	Status ReleaseStatus `json:"status,omitzero"`
}

// +kubebuilder:object:root=true

// ReleaseList contains a list of Release
type ReleaseList struct {
	metav1.TypeMeta `json:",inline"`
	metav1.ListMeta `json:"metadata,omitzero"`
	Items           []Release `json:"items"`
}

func init() {
	SchemeBuilder.Register(&Release{}, &ReleaseList{})
}
