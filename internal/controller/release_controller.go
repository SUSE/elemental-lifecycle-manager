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
	"context"
	"errors"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
)

const requeueInterval = 30 * time.Second

// ReleaseReconciler reconciles a Release object
type ReleaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=lifecycle.suse.com,resources=releases,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=lifecycle.suse.com,resources=releases/status,verbs=get;update;patch
// +kubebuilder:rbac:groups=lifecycle.suse.com,resources=releases/finalizers,verbs=update
// +kubebuilder:rbac:groups="",resources=nodes,verbs=watch;list
// +kubebuilder:rbac:groups="",resources=configmaps,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups="",resources=secrets,verbs=get;list;watch
// +kubebuilder:rbac:groups=upgrade.cattle.io,resources=plans,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=helm.cattle.io,resources=helmcharts,verbs=get;list;watch;create;update;patch
// +kubebuilder:rbac:groups=batch,resources=jobs,verbs=get;list;watch
// +kubebuilder:rbac:urls=/version,verbs=get

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
func (r *ReleaseReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Reconciling Release")

	release := &lifecyclev1alpha1.Release{}
	if err := r.Get(ctx, req.NamespacedName, release); err != nil {
		logger.Error(err, "unable to fetch Release")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	// Store original Release resource status before any reconciliation has been done
	originalStatus := release.Status.DeepCopy()
	result, err := r.reconcileNormal(ctx, release)

	// Ensure that a change to the state of the Release resource has actually been done
	// before doing any Release resource updates
	if !equality.Semantic.DeepEqual(*originalStatus, release.Status) {
		if statusErr := r.updateReleaseStatus(ctx, req.NamespacedName, release.Status); statusErr != nil {
			return ctrl.Result{}, errors.Join(err, statusErr)
		}
	}

	// Handle reconcileNormal errors only when the Release status has been updated (if needed).
	if err != nil {
		return ctrl.Result{}, err
	}

	return result, nil
}

// TODO: remove linter skip once function begins to actually return errors
//
//nolint:unparam
func (r *ReleaseReconciler) reconcileNormal(ctx context.Context, release *lifecyclev1alpha1.Release) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Upgrade to the platform requested",
		"version", release.Spec.Version,
		"registry", release.Spec.Registry)

	if release.Status.ObservedGeneration != release.Generation {
		release.Status.ObservedGeneration = release.Generation
		release.Status.Conditions = nil
		// TODO: re-initialise 'Pending' conditions

		return ctrl.Result{Requeue: true}, nil
	}

	// TODO: update 'Applied' condition
	// TODO: retrieve release manifest
	// TODO: parse configuration options from manifest
	// TODO: call upgrade specific reconcilers
	// TODO: update necessary conditions
	return ctrl.Result{RequeueAfter: requeueInterval}, nil
}

// updateReleaseStatus persists the specified release status using the latest Release resource state.
// Additionally it retries when hitting a release status update conflict, as the Release resource may have been modified
// between the reconciler's initial fetch and the status update.
func (r *ReleaseReconciler) updateReleaseStatus(ctx context.Context, name types.NamespacedName, status lifecyclev1alpha1.ReleaseStatus) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &lifecyclev1alpha1.Release{}
		if err := r.Get(ctx, name, latest); err != nil {
			return client.IgnoreNotFound(err)
		}

		latest.Status = *status.DeepCopy()
		return r.Status().Update(ctx, latest)
	})
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lifecyclev1alpha1.Release{}).
		Named("release").
		Complete(r)
}
