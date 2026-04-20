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
	"context"
	"fmt"

	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/util/version"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	logf "sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/controller-runtime/pkg/webhook/admission"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
)

// nolint:unused
// log is for logging in this package.
var releaselog = logf.Log.WithName("release-resource")

// SetupReleaseWebhookWithManager registers the webhook for Release in the manager.
func SetupWebhookWithManager(mgr ctrl.Manager) error {
	return ctrl.NewWebhookManagedBy(mgr, &lifecyclev1alpha1.Release{}).
		WithValidator(&ReleaseValidator{client: mgr.GetClient()}).
		Complete()
}

// +kubebuilder:webhook:path=/validate-lifecycle-suse-com-v1alpha1-release,mutating=false,failurePolicy=fail,sideEffects=None,groups=lifecycle.suse.com,resources=releases,verbs=create;update;delete,versions=v1alpha1,name=vrelease-v1alpha1.kb.io,admissionReviewVersions=v1

type ReleaseValidator struct {
	client client.Client
}

// ValidateCreate ensures that there is always a single Release resource deployed on the cluster, effectively ensuring that
// no clash between two Release resources happens.
func (r *ReleaseValidator) ValidateCreate(ctx context.Context, release *lifecyclev1alpha1.Release) (admission.Warnings, error) {
	if release.Spec.Registry == "" {
		return nil, fmt.Errorf("release registry is required")
	}

	if _, err := validateReleaseVersion(release.Spec.Version); err != nil {
		return nil, fmt.Errorf("validating release version: %w", err)
	}

	releaseList := &lifecyclev1alpha1.ReleaseList{}
	if err := r.client.List(ctx, releaseList); err != nil {
		return nil, fmt.Errorf("listing existing releases: %w", err)
	}

	if len(releaseList.Items) > 0 {
		return nil, fmt.Errorf(
			"cannot create release %s. The cluster has an already created Release object: %s/%s",
			release.Name,
			releaseList.Items[0].Namespace,
			releaseList.Items[0].Name,
		)
	}

	return nil, nil
}

// ValidateUpdate ensures that updates over a Release resource can only be done when no upgrade process is currently running.
func (r *ReleaseValidator) ValidateUpdate(_ context.Context, oldRelease, newRelease *lifecyclev1alpha1.Release) (admission.Warnings, error) {
	newReleaseVersion, err := validateReleaseVersion(newRelease.Spec.Version)
	if err != nil {
		return nil, err
	}

	if newRelease.Spec.Registry == "" {
		return nil, fmt.Errorf("release registry is required")
	}

	if err = validateNoUpgradeInProgress(oldRelease); err != nil {
		return nil, err
	}

	if oldRelease.Status.Version != "" {
		indicator, err := newReleaseVersion.Compare(oldRelease.Status.Version)
		if err != nil {
			return nil, fmt.Errorf("comparing versions: %w", err)
		}

		switch indicator {
		case 0:
			return nil, fmt.Errorf("any edits over '%s' must come with an increment of the version", newRelease.Name)
		case -1:
			return nil, fmt.Errorf("new version must be greater than the currently applied one ('%s')", oldRelease.Status.Version)
		}
	}

	return nil, nil
}

// ValidateDelete ensures that once a Release resource is created it cannot be accidentally deleted.
func (r *ReleaseValidator) ValidateDelete(_ context.Context, _ *lifecyclev1alpha1.Release) (admission.Warnings, error) {
	return nil, fmt.Errorf("deleting release objects is not allowed")
}

// validateNoUpgradeInProgress checks if an upgrade is currently in progress.
// Returns an error if the Applied condition is not True (upgrade in progress or failed).
func validateNoUpgradeInProgress(release *lifecyclev1alpha1.Release) error {
	appliedCond := apimeta.FindStatusCondition(release.Status.Conditions, lifecyclev1alpha1.ConditionApplied)

	switch {
	case appliedCond == nil:
		// No Applied condition means upgrade hasn't started yet, allow edits
		return nil
	case appliedCond.Status == metav1.ConditionTrue:
		// Previous upgrade completed successfully, allow edits
		return nil
	case appliedCond.Reason == lifecyclev1alpha1.UpgradeFailed:
		// Previous upgrade completed but failed, allow edits
		return nil
	}

	return fmt.Errorf("cannot edit while upgrade is in '%s' state", appliedCond.Reason)
}

// validateReleaseVersion checks the provided version string by first attempting
// to parse it as a semantic version. If that fails, it falls back to parsing it
// as a generic version format. An error is returned if both attempts fail.
func validateReleaseVersion(releaseVersion string) (*version.Version, error) {
	if releaseVersion == "" {
		return nil, fmt.Errorf("release version is required")
	}

	v, err := version.Parse(releaseVersion)
	if err != nil {
		return nil, fmt.Errorf("'%s' is not a semantic version", releaseVersion)
	}

	return v, nil
}
