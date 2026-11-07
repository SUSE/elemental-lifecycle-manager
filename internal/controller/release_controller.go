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

	"github.com/suse/elemental/v3/pkg/manifest/api"
	"go.yaml.in/yaml/v3"
	"k8s.io/apimachinery/pkg/api/equality"
	apimeta "k8s.io/apimachinery/pkg/api/meta"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	k8sruntime "k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/client-go/util/retry"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/handler"
	"sigs.k8s.io/controller-runtime/pkg/log"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/plan"
	releasecache "github.com/suse/elemental-lifecycle-manager/internal/release"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
	corev1 "k8s.io/api/core/v1"
)

const requeueInterval = 30 * time.Second

// ReleaseReconciler reconciles a Release object
type ReleaseReconciler struct {
	client.Client
	Scheme *k8sruntime.Scheme

	RetrieveManifest   func(ctx context.Context, registry, version string) (*resolver.ResolvedManifest, error)
	Pipeline           *upgrade.Pipeline
	AutoUpgradeHandler upgrade.PhaseHandler

	// RawManifests holds the raw core manifest used by the LCM self-upgrade phase. Ensures that between
	// self-upgrade reconciliations we do not needlessly fetch the core manifest again.
	RawManifests map[string][]byte

	// ChartVersion is the version of the Elemental Lifecycle Manager chart that this process was deployed from.
	ChartVersion string
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

		return ctrl.Result{RequeueAfter: time.Second}, nil
	}

	defer updateAppliedCondition(release, r.Pipeline.Phases())

	if !apimeta.IsStatusConditionTrue(release.Status.Conditions, upgrade.PhaseLCM.ConditionType()) {
		status, err := r.autoUpgrade(ctx, release)
		if err != nil {
			return ctrl.Result{}, err
		}

		if s := status.State; s != lifecyclev1alpha1.UpgradeSucceeded && s != lifecyclev1alpha1.UpgradeSkipped {
			if s == lifecyclev1alpha1.UpgradeFailed {
				return ctrl.Result{}, nil
			}
			return ctrl.Result{RequeueAfter: requeueInterval}, nil
		}
	}

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
		if configErr, ok := errors.AsType[upgrade.ConfigError](err); ok {
			logger.Info("Invalid upgrade configuration", "error", configErr.Error())

			setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionFalse,
				lifecyclev1alpha1.UpgradeFailed, configErr.Error())
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, fmt.Errorf("parsing upgrade config: %w", err)
	}

	// Clean up plans from previous versions before creating new ones
	if release.Status.Version != "" && release.Status.Version != config.ReleaseVersion {
		if err := r.cleanupOldVersionPlans(ctx, release.Name, release.Status.Version); err != nil {
			logger.Error(err, "Failed to cleanup old version plans", "oldVersion", release.Status.Version)
			return ctrl.Result{}, fmt.Errorf("cleaning up plans for version %s: %w", release.Status.Version, err)
		}
	}

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

// autoUpgrade upgrades the LCM charts to the target release and returns the resulting LCM phase status.
func (r *ReleaseReconciler) autoUpgrade(ctx context.Context, release *lifecyclev1alpha1.Release) (*upgrade.PhaseStatus, error) {
	config, err := r.prepareAutoUpgradeConfig(ctx, release)
	if err != nil {
		if configErr, ok := errors.AsType[upgrade.ConfigError](err); ok {
			log.FromContext(ctx).Info("Invalid LCM upgrade configuration", "error", configErr.Error())
			setCondition(release, lifecyclev1alpha1.ConditionApplied, metav1.ConditionFalse,
				lifecyclev1alpha1.UpgradeFailed, configErr.Error())
			return &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeFailed, Message: configErr.Error()}, nil
		}
		return nil, fmt.Errorf("preparing LCM upgrade config: %w", err)
	}

	status, err := r.AutoUpgradeHandler.Reconcile(ctx, config)
	if err != nil {
		log.FromContext(ctx).Error(err, "LCM upgrade failed")
		setCondition(release, upgrade.PhaseLCM.ConditionType(), metav1.ConditionFalse, lifecyclev1alpha1.UpgradeFailed, err.Error())
		return &upgrade.PhaseStatus{State: lifecyclev1alpha1.UpgradeFailed, Message: err.Error()}, nil
	}

	// Safeguard against prematurely reporting a successful upgrade before the target LCM version is running.
	// TODO(ivpe): confirmTakeover only compares chart versions. If the new LCM process updates its own chart
	// (e.g. to apply a runtime property the previous process did not know about), the resulting rollout would
	// not be detected and the outgoing process may start the upgrade pipeline with its previous configuration.
	// Based on this, a more sophisticated guard would be needed to be implemented long-term.
	status = r.confirmTakeover(status, config)
	updatePhaseCondition(release, upgrade.PhaseLCM, status)
	return status, nil
}

// confirmTakeover confirms whether the current LCM process is running a version that is defined in the desired
// upgrade config. Returns 'InProgress' when the LCM charts are upgraded but the current process runs a different
// version, indicating that the LCM pod has not yet been replaced. In all other cases the status is returned unchanged.
func (r *ReleaseReconciler) confirmTakeover(status *upgrade.PhaseStatus, config *upgrade.Config) *upgrade.PhaseStatus {
	if status.State != lifecyclev1alpha1.UpgradeSucceeded || r.ChartVersion == "" {
		return status
	}

	for _, c := range config.HelmCharts[upgrade.PhaseLCM] {
		if c.Chart.Chart == upgrade.LCMChartName && c.Chart.Version != r.ChartVersion {
			return &upgrade.PhaseStatus{
				State:   lifecyclev1alpha1.UpgradeInProgress,
				Message: fmt.Sprintf("Waiting for a running process with %s version", c.Chart.Version),
			}
		}
	}

	return status
}

func runtimeHelmChartConfigs(release *lifecyclev1alpha1.Release) map[string]upgrade.RuntimeHelmChartConfig {
	componentConfig := release.Spec.ComponentConfig
	if componentConfig == nil {
		return nil
	}

	var configs map[string]upgrade.RuntimeHelmChartConfig
	for _, c := range componentConfig.Helm {
		var secretRef *upgrade.RuntimeSecretValueSource
		if ref := c.ValuesFrom.SecretRef; ref != nil {
			secretRef = &upgrade.RuntimeSecretValueSource{Name: ref.Name, Keys: ref.Keys}
		}

		if configs == nil {
			configs = make(map[string]upgrade.RuntimeHelmChartConfig)
		}

		configs[c.Chart] = upgrade.RuntimeHelmChartConfig{
			PriorityUpgrade: c.PriorityUpgrade,
			Values:          c.Values,
			ValuesFrom:      upgrade.RuntimeHelmChartValuesFrom{SecretRef: secretRef},
		}
	}
	return configs
}

// prepareAutoUpgradeConfig parses a Release instance and creates an upgrade.Config for the purpose of self-upgrading LCM
// by focusing only on LCM charts.
func (r *ReleaseReconciler) prepareAutoUpgradeConfig(ctx context.Context, release *lifecyclev1alpha1.Release) (*upgrade.Config, error) {
	data, err := r.getOrRetrieveRawCoreManifest(ctx, release)
	if err != nil {
		return nil, err
	}

	var core struct {
		Components struct {
			Helm struct {
				Charts []struct {
					Chart      string         `yaml:"chart"`
					Repository string         `yaml:"repository"`
					Version    string         `yaml:"version"`
					Values     map[string]any `yaml:"values"`
					DependsOn  []struct {
						Name string `yaml:"name"`
						Type string `yaml:"type"`
					} `yaml:"dependsOn"`
				} `yaml:"charts"`
				Repositories []struct {
					Name string `yaml:"name"`
					URL  string `yaml:"url"`
				} `yaml:"repositories"`
			} `yaml:"helm"`
		} `yaml:"components"`
	}

	if err := yaml.Unmarshal(data, &core); err != nil {
		return nil, fmt.Errorf("parsing partial core manifest: %w", err)
	}

	var helm api.Helm
	for _, c := range core.Components.Helm.Charts {
		deps := make([]api.HelmChartDependency, 0, len(c.DependsOn))
		for _, d := range c.DependsOn {
			deps = append(deps, api.HelmChartDependency{Name: d.Name, Type: api.DependencyType(d.Type)})
		}

		helm.Charts = append(helm.Charts, &api.HelmChart{
			Chart: c.Chart, Repository: c.Repository, Version: c.Version, Values: c.Values, DependsOn: deps,
		})
	}

	for _, repo := range core.Components.Helm.Repositories {
		helm.Repositories = append(helm.Repositories, &api.HelmRepository{Name: repo.Name, URL: repo.URL})
	}

	return upgrade.NewSelfUpgradeConfig(
		types.NamespacedName{Name: release.Name, Namespace: release.Namespace},
		release.Spec.Version,
		&helm,
		runtimeHelmChartConfigs(release),
	)
}

// getOrRetrieveRawCoreManifest returns the raw core manifest bytes for the Release that triggered the upgrade.
func (r *ReleaseReconciler) getOrRetrieveRawCoreManifest(ctx context.Context, release *lifecyclev1alpha1.Release) ([]byte, error) {
	imageRef := fmt.Sprintf("%s:%s", release.Spec.Registry, release.Spec.Version)
	if data, ok := r.RawManifests[imageRef]; ok {
		return data, nil
	}

	data, err := releasecache.ReadRawCoreManifest(ctx, imageRef)
	if err != nil {
		return nil, fmt.Errorf("reading core manifest: %w", err)
	}

	r.RawManifests[imageRef] = data
	return data, nil
}

// updateReleaseStatus persists the specified release status using the latest Release resource state.
// Additionally it retries when hitting a release status update conflict, as the Release resource may have been modified
// between the reconciler's initial fetch and the status update.
func (r *ReleaseReconciler) updateReleaseStatus(ctx context.Context, namespacedName types.NamespacedName, status lifecyclev1alpha1.ReleaseStatus) error {
	return retry.RetryOnConflict(retry.DefaultRetry, func() error {
		latest := &lifecyclev1alpha1.Release{}
		if err := r.Get(ctx, namespacedName, latest); err != nil {
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

// parseDrainOpts determines which node groups should be drained during the upgrade.
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
		if _, isControlPlane := node.Labels[plan.ControlPlaneLabel]; isControlPlane {
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

	return upgrade.NewConfig(manifest,
		release.Spec.Version,
		types.NamespacedName{Name: release.Name, Namespace: release.Namespace},
		&upgrade.RuntimeConfig{DrainOpts: opts, HelmCharts: runtimeHelmChartConfigs(release)})
}

// cleanupOldVersionPlans deletes SUC Plans from a previous version.
func (r *ReleaseReconciler) cleanupOldVersionPlans(ctx context.Context, releaseName, oldVersion string) error {
	logger := log.FromContext(ctx)
	logger.Info("Cleaning up old version plans",
		"release", releaseName,
		"oldVersion", oldVersion)

	planList := &upgradecattlev1.PlanList{}
	if err := r.List(ctx, planList,
		client.InNamespace(plan.Namespace),
		client.MatchingLabels{
			lifecyclev1alpha1.ReleaseNameLabel:    releaseName,
			lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(oldVersion),
		},
	); err != nil {
		return fmt.Errorf("listing old plans: %w", err)
	}

	var deleteErrs []error
	for _, p := range planList.Items {
		logger.Info("Deleting old plan", "name", p.Name)
		if err := r.Delete(ctx, &p); err != nil {
			deleteErrs = append(deleteErrs, fmt.Errorf("deleting plan %s: %w", p.Name, err))
		}
	}

	return errors.Join(deleteErrs...)
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

// mapHelmChartToRelease maps HelmChart events to Release reconcile requests.
// Uses the release name label on the HelmChart to find the corresponding Release.
func (r *ReleaseReconciler) mapHelmChartToRelease(ctx context.Context, obj client.Object) []ctrl.Request {
	// Only watch HelmCharts in the namespace where we create them
	if obj.GetNamespace() != reconcilers.HelmChartNamespace {
		return nil
	}

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
		// Ensure we reconcile on any event change relating to HelmCharts created/updated by the controller
		Watches(&helmv1.HelmChart{}, handler.EnqueueRequestsFromMapFunc(r.mapHelmChartToRelease)).
		Named("release").
		Complete(r)
}
