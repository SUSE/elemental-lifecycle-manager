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
	"fmt"
	"time"

	"k8s.io/apimachinery/pkg/api/equality"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	releasecache "github.com/suse/elemental-lifecycle-manager/internal/release"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
	corev1 "k8s.io/api/core/v1"
)

const requeueInterval = 30 * time.Second

// ReleaseReconciler reconciles a Release object
type ReleaseReconciler struct {
	client.Client
	Scheme *runtime.Scheme

	RetrieveManifest func(ctx context.Context, registry, version string) (*resolver.ResolvedManifest, error)
	Pipeline         *upgrade.Pipeline
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

func (r *ReleaseReconciler) reconcileNormal(ctx context.Context, release *lifecyclev1alpha1.Release) (ctrl.Result, error) {
	logger := log.FromContext(ctx)
	logger.Info("Upgrade to the platform requested",
		"version", release.Spec.Version,
		"registry", release.Spec.Registry)

	if release.Status.ObservedGeneration != release.Generation {
		release.Status.ObservedGeneration = release.Generation
		release.Status.Conditions = nil
		initializePendingConditions(release, r.Pipeline.Phases())

		return ctrl.Result{Requeue: true}, nil
	}

	defer updateAppliedCondition(release, r.Pipeline.Phases())

	manifest, err := r.getOrRetrieveManifest(ctx, release)
	if err != nil {
		setCondition(release, lifecyclev1alpha1.ConditionManifestResolved, metav1.ConditionFalse,
			lifecyclev1alpha1.UpgradeFailed, fmt.Sprintf("Failed to retrieve release manifest: %v", err))
		return ctrl.Result{}, fmt.Errorf("retrieving release manifest: %w", err)
	}

	setCondition(release, lifecyclev1alpha1.ConditionManifestResolved, metav1.ConditionTrue,
		lifecyclev1alpha1.UpgradeSucceeded, "Release manifest retrieved successfully")

	config, err := r.parseUpgradeConfig(ctx, manifest, release)
	if err != nil {
		return ctrl.Result{}, fmt.Errorf("parsing upgrade config: %w", err)
	}

	// TODO: cleanup SUC Plans for previous release

	result, err := r.Pipeline.Reconcile(ctx, config)
	if err != nil {
		setPhaseConditionFromError(release, err)
		return ctrl.Result{}, fmt.Errorf("reconciling upgrade: %w", err)
	}

	updatePhaseConditions(release, result)

	if result.AllComplete() {
		release.Status.Version = config.ReleaseVersion
		return ctrl.Result{}, nil
	}

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

// getOrRetrieveManifest returns a cached manifest or fetches it from the registry.
func (r *ReleaseReconciler) getOrRetrieveManifest(ctx context.Context, release *lifecyclev1alpha1.Release) (*resolver.ResolvedManifest, error) {
	logger := log.FromContext(ctx)
	cache := &releasecache.ManifestCache{Client: r.Client}

	manifest, err := cache.Get(ctx, release.Namespace, release.Spec.Version)
	if err != nil {
		logger.Error(err, "Failed to get cached manifest, will fetch from registry")
	}
	if manifest != nil {
		logger.Info("Using cached release manifest", "version", release.Spec.Version)
		return manifest, nil
	}

	manifest, err = r.RetrieveManifest(ctx, release.Spec.Registry, release.Spec.Version)
	if err != nil {
		return nil, err
	}

	if err := cache.Set(ctx, release.Namespace, release.Spec.Version, manifest); err != nil {
		logger.Error(err, "Failed to cache manifest, continuing without caching")
	}

	return manifest, nil
}

func (r *ReleaseReconciler) parseDrainOpts(ctx context.Context, release *lifecyclev1alpha1.Release) (*upgrade.DrainOpts, error) {
	if release.Spec.DisableDrain {
		return &upgrade.DrainOpts{ControlPlane: false, Worker: false}, nil
	}

	nodeList := &corev1.NodeList{}
	if err := r.List(ctx, nodeList); err != nil {
		return nil, fmt.Errorf("listing nodes: %w", err)
	}

	var controlPlaneCounter, workerCounter int
	for _, node := range nodeList.Items {
		// TODO: move control-plane label under 'plan' package when SUC plan logic is introduced
		if _, isControlPlane := node.Labels["node-role.kubernetes.io/control-plane"]; isControlPlane {
			controlPlaneCounter++
		} else {
			workerCounter++
		}
	}

	switch {
	case controlPlaneCounter > 1 && workerCounter <= 1:
		return &upgrade.DrainOpts{ControlPlane: true, Worker: false}, nil
	case controlPlaneCounter == 1 && workerCounter > 1:
		return &upgrade.DrainOpts{ControlPlane: false, Worker: true}, nil
	case controlPlaneCounter <= 1 && workerCounter <= 1:
		return &upgrade.DrainOpts{ControlPlane: false, Worker: false}, nil
	default:
		return &upgrade.DrainOpts{ControlPlane: true, Worker: true}, nil
	}
}

func (r *ReleaseReconciler) parseUpgradeConfig(ctx context.Context, manifest *resolver.ResolvedManifest, release *lifecyclev1alpha1.Release) (config *upgrade.Config, err error) {
	opts, err := r.parseDrainOpts(ctx, release)
	if err != nil {
		return nil, fmt.Errorf("parsing drain options: %w", err)
	}

	return upgrade.NewConfig(manifest, release.Spec.Version, types.NamespacedName{Name: release.Name, Namespace: release.Namespace}, opts)
}

// mapPlanToRelease maps SUC Plan events to Release reconcile requests.
// Uses the release name label on the Plan to find the corresponding Release.
func (r *ReleaseReconciler) mapPlanToRelease(ctx context.Context, obj client.Object) []ctrl.Request {
	releaseName := obj.GetLabels()[lifecyclev1alpha1.ReleaseNameLabel]
	if releaseName == "" {
		return nil
	}

	// Release resources are cluster-scoped, so reconcile requests do not include
	// a namespace to use with Get. Since the Release webhook ensures there is only
	// one Release per cluster, it is safe to use List to retrieve the active Release.
	releaseList := &lifecyclev1alpha1.ReleaseList{}
	if err := r.List(ctx, releaseList); err != nil {
		return nil
	}

	for _, rel := range releaseList.Items {
		if rel.Name == releaseName {
			return []ctrl.Request{{
				NamespacedName: client.ObjectKeyFromObject(&rel),
			}}
		}
	}

	return nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *ReleaseReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&lifecyclev1alpha1.Release{}).
		// Ensure we reconcile on any event change relating to SUC Plans created by the controller
		Watches(&upgradecattlev1.Plan{}, handler.EnqueueRequestsFromMapFunc(r.mapPlanToRelease)).
		Named("release").
		Complete(r)
}
