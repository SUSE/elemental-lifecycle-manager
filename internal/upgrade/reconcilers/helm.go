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

package reconcilers

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"slices"
	"strings"

	"go.yaml.in/yaml/v3"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/equality"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"sigs.k8s.io/controller-runtime/pkg/log"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

const (
	// HelmChartNamespace is the namespace where HelmChart resources are created.
	// The Helm Controller watches for HelmChart resources in this namespace.
	HelmChartNamespace = "kube-system"
)

// chartUpgradeResult holds the result of a chart upgrade attempt.
type chartUpgradeResult struct {
	chartName string
	state     helm.ChartState
}

// HelmReconciler reconciles HelmChart resources for Helm chart deployments.
type HelmReconciler struct {
	client.Client
	helmClient helm.Client
	// releaseName is the name of the Release resource managing these charts
	releaseName string
	// releaseVersion is the target release version
	releaseVersion string
}

// NewHelmReconciler creates a new Helm reconciler.
func NewHelmReconciler(c client.Client, h helm.Client) *HelmReconciler {
	return &HelmReconciler{
		Client:     c,
		helmClient: h,
	}
}

func (r *HelmReconciler) Phase() upgrade.Phase {
	return upgrade.PhaseHelmCharts
}

func (r *HelmReconciler) Reconcile(ctx context.Context, config *upgrade.Config) (*upgrade.PhaseStatus, error) {
	if config == nil || config.HelmCharts == nil || len(config.HelmCharts) == 0 {
		return r.Phase().SkippedStatus(), nil
	}

	return r.reconcileHelmCharts(ctx, config.ReleaseNamespacedName.Name, config.ReleaseVersion, config.HelmCharts)
}

// reconcileHelmCharts ensures the HelmChart resources exist and are up to date.
// Only charts that are already installed on the cluster will be upgraded.
// Charts are processed in dependency order.
func (r *HelmReconciler) reconcileHelmCharts(ctx context.Context, releaseName, releaseVersion string, chartConfigs []*upgrade.HelmChartConfig) (*upgrade.PhaseStatus, error) {
	logger := log.FromContext(ctx)

	// Store release context for labeling HelmChart resources
	r.releaseName = releaseName
	r.releaseVersion = releaseVersion

	var workloadCharts []*upgrade.HelmChartConfig
	for _, chartCfg := range chartConfigs {
		name := chartCfg.Chart.GetName()
		if !isLCMChart(name) {
			workloadCharts = append(workloadCharts, chartCfg)
		}
	}

	orderedChartConfigs, err := sortChartConfigsByDependencies(workloadCharts)
	if err != nil {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeFailed,
			Message: fmt.Sprintf("Failed to resolve chart dependencies: %v", err),
		}, err
	}

	logger.Info("Reconciling Helm charts", "count", len(orderedChartConfigs))

	var results []chartUpgradeResult
	for _, chartConfig := range orderedChartConfigs {
		chartName := chartConfig.Chart.GetName()
		state, err := r.reconcileChart(ctx, chartConfig)
		if err != nil {
			return &upgrade.PhaseStatus{
				State:   lifecyclev1alpha1.UpgradeFailed,
				Message: fmt.Sprintf("Failed to reconcile chart %s: %v", chartName, err),
			}, err
		}

		results = append(results, chartUpgradeResult{
			chartName: chartName,
			state:     state,
		})

		// If a chart is in progress, we need to wait before processing dependents
		if state == helm.ChartStateInProgress {
			logger.Info("Chart upgrade in progress, waiting", "chart", chartName)
			break
		}
	}

	return aggregateResults(results, len(orderedChartConfigs), "Helm"), nil
}

// sortChartConfigsByDependencies returns a sorted slice of chart configurations,
// where configuration for chart dependencies come before their respective dependent chart configurations.
func sortChartConfigsByDependencies(chartConfigs []*upgrade.HelmChartConfig) ([]*upgrade.HelmChartConfig, error) {
	chartConfigMap := make(map[string]*upgrade.HelmChartConfig)
	for _, chartConfig := range chartConfigs {
		chartConfigMap[chartConfig.Chart.GetName()] = chartConfig
	}

	// Track visited and in-progress for cycle detection
	visited, inProgress := make(map[string]bool), make(map[string]bool)
	var result []*upgrade.HelmChartConfig

	var visit func(name string) error
	visit = func(name string) error {
		if inProgress[name] {
			return fmt.Errorf("circular dependency detected involving chart %s", name)
		}
		if visited[name] {
			return nil
		}

		chartConfig, exists := chartConfigMap[name]
		if !exists {
			// Chart not in our list, skip (it might be an external dependency)
			return nil
		}

		inProgress[name] = true

		// Visit dependencies first
		for _, dep := range chartConfig.Chart.DependsOn {
			if dep.Type == api.DependencyTypeHelm {
				if err := visit(dep.Name); err != nil {
					return err
				}
			}
		}

		inProgress[name] = false
		visited[name] = true
		result = append(result, chartConfig)

		return nil
	}

	for _, chartConfig := range chartConfigs {
		if err := visit(chartConfig.Chart.GetName()); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// reconcileChart reconciles a single chart and returns its state.
// If the chart is not installed on the cluster, it is skipped.
func (r *HelmReconciler) reconcileChart(ctx context.Context, chartConfig *upgrade.HelmChartConfig) (helm.ChartState, error) {
	logger := log.FromContext(ctx)
	chart := chartConfig.Chart
	chartName := chart.GetName()

	// Check if chart is installed on the cluster
	helmRelease, err := r.helmClient.RetrieveRelease(chartName)
	if err != nil {
		if errors.Is(err, helm.ErrReleaseNotFound) {
			logger.Info("Chart not installed on cluster, skipping", "chart", chartName)
			return helm.ChartStateNotInstalled, nil
		}
		return helm.ChartStateUnknown, fmt.Errorf("retrieving helm release: %w", err)
	}

	// Check for existing HelmChart CR
	existing := &helmv1.HelmChart{}
	err = r.Get(ctx, types.NamespacedName{
		Name:      chartName,
		Namespace: HelmChartNamespace,
	}, existing)

	if apierrors.IsNotFound(err) {
		if helmRelease.ChartVersion == chart.Version {
			logger.Info("Chart already at target version", "chart", chartName, "version", chart.Version)
			return helm.ChartStateVersionAlreadyInstalled, nil
		}

		// Create new HelmChart CR from existing release
		logger.Info("Creating HelmChart CR for upgrade", "chart", chartName,
			"currentVersion", helmRelease.ChartVersion,
			"targetVersion", chart.Version)
		return helm.ChartStateInProgress, r.createHelmChartFromRelease(ctx, chartConfig, helmRelease)
	}

	if err != nil {
		return helm.ChartStateUnknown, fmt.Errorf("getting HelmChart: %w", err)
	}

	update, err := r.shouldUpdateChart(existing, chartConfig)
	if err != nil {
		return helm.ChartStateUnknown, fmt.Errorf("checking whether chart update is necessary: %w", err)
	}

	if update {
		logger.Info("Updating HelmChart", "chart", chartName,
			"currentVersion", existing.Spec.Version,
			"targetVersion", chart.Version)
		return helm.ChartStateInProgress, r.updateHelmChart(ctx, chartConfig, existing)
	}

	// HelmChart exists with target version, check job status
	return r.evaluateHelmChartJobStatus(ctx, existing)
}

// createHelmChartFromRelease creates a HelmChart CR from an existing Helm release.
func (r *HelmReconciler) createHelmChartFromRelease(ctx context.Context, chartConfig *upgrade.HelmChartConfig, release *helm.ReleaseInfo) error {
	helmChart, err := r.buildHelmChart(chartConfig, release.Namespace)
	if err != nil {
		return fmt.Errorf("building HelmChart: %w", err)
	}

	// Merge custom helm release values with any values defined during the build of the HelmChart resource.
	mergedValues, err := helm.MergeHelmValues(release.Config, helmChart.Spec.ValuesContent)
	if err != nil {
		return fmt.Errorf("merging HelmChart values: %w", err)
	}

	if mergedValues != nil {
		rawValues, err := yaml.Marshal(mergedValues)
		if err != nil {
			return fmt.Errorf("marshaling HelmChart values: %w", err)
		}
		helmChart.Spec.ValuesContent = string(rawValues)
	}

	return r.Create(ctx, helmChart)
}

// updateHelmChart updates an existing HelmChart CR to trigger an upgrade.
func (r *HelmReconciler) updateHelmChart(ctx context.Context, chartConfig *upgrade.HelmChartConfig, existing *helmv1.HelmChart) error {
	chart := chartConfig.Chart
	repoURL, err := r.resolveRepositoryURL(chartConfig)
	if err != nil {
		return fmt.Errorf("parsing repository URL for chart %q: %w", chart.Name, err)
	}

	// Ensure labels are set for Release tracking
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	existing.Labels[lifecyclev1alpha1.ReleaseNameLabel] = r.releaseName
	existing.Labels[lifecyclev1alpha1.ReleaseVersionLabel] = lifecyclev1alpha1.SanitizeVersion(r.releaseVersion)

	existing.Spec.Version = chart.Version
	if repoURL.Scheme == "oci" {
		// HelmChart's referencing OCI images expect the full OCI reference to be
		// specified under Spec.Chart and for Spec.Repo to be empty.
		existing.Spec.Chart = fmt.Sprintf("%s/%s", strings.TrimSuffix(repoURL.String(), "/"), chart.Chart)
		existing.Spec.Repo = ""
	} else {
		existing.Spec.Chart = chart.Chart
		existing.Spec.Repo = repoURL.String()
	}

	if err := r.resolveHelmChartValues(existing, chartConfig); err != nil {
		return fmt.Errorf("resolving HelmChart values: %w", err)
	}

	return r.Update(ctx, existing)
}

// buildHelmChart creates a HelmChart resource from the manifest chart definition.
func (r *HelmReconciler) buildHelmChart(chartConfig *upgrade.HelmChartConfig, targetNamespace string) (*helmv1.HelmChart, error) {
	chart := chartConfig.Chart
	name := chart.GetName()
	repoURL, err := r.resolveRepositoryURL(chartConfig)
	if err != nil {
		return nil, fmt.Errorf("parsing repository URL for chart %q: %w", chart.Name, err)
	}

	if targetNamespace == "" {
		targetNamespace = chart.Namespace
	}

	backoffLimit := int32(6)

	helmChart := &helmv1.HelmChart{
		TypeMeta: metav1.TypeMeta{
			APIVersion: "helm.cattle.io/v1",
			Kind:       "HelmChart",
		},
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: HelmChartNamespace,
			Labels: map[string]string{
				lifecyclev1alpha1.ReleaseNameLabel:    r.releaseName,
				lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(r.releaseVersion),
			},
		},
		Spec: helmv1.HelmChartSpec{
			Version:         chart.Version,
			TargetNamespace: targetNamespace,
			BackOffLimit:    &backoffLimit,
		},
	}

	if repoURL.Scheme == "oci" {
		// HelmChart's referencing OCI images expect the full OCI reference to be
		// specified under Spec.Chart and for Spec.Repo to be empty.
		helmChart.Spec.Chart = fmt.Sprintf("%s/%s", strings.TrimSuffix(repoURL.String(), "/"), chart.Chart)
		helmChart.Spec.Repo = ""
	} else {
		helmChart.Spec.Chart = chart.Chart
		helmChart.Spec.Repo = repoURL.String()
	}

	if err := r.resolveHelmChartValues(helmChart, chartConfig); err != nil {
		return nil, fmt.Errorf("resolving HelmChart values: %w", err)
	}

	return helmChart, nil
}

// resolveRepositoryURL resolves and parses the repository URL for a chart.
func (r *HelmReconciler) resolveRepositoryURL(chartConfig *upgrade.HelmChartConfig) (*url.URL, error) {
	repositoryURL := chartConfig.Chart.Repository

	if chartConfig.Repository != nil {
		repositoryURL = chartConfig.Repository.URL
	}

	return url.Parse(repositoryURL)
}

// evaluateHelmChartJobStatus checks the status of the Helm upgrade job.
func (r *HelmReconciler) evaluateHelmChartJobStatus(ctx context.Context, chart *helmv1.HelmChart) (helm.ChartState, error) {
	if chart.Status.JobName == "" {
		return helm.ChartStateInProgress, nil
	}

	job := &batchv1.Job{}
	if err := r.Get(ctx, types.NamespacedName{
		Name:      chart.Status.JobName,
		Namespace: HelmChartNamespace,
	}, job); err != nil {
		if apierrors.IsNotFound(err) {
			// Job completed and was cleaned up - check conditions
			for _, cond := range chart.Status.Conditions {
				if cond.Type == "Failed" && cond.Status == corev1.ConditionTrue {
					return helm.ChartStateFailed, nil
				}
			}
			return helm.ChartStateSucceeded, nil
		}
		return helm.ChartStateUnknown, err
	}

	// Check job conditions
	idx := slices.IndexFunc(job.Status.Conditions, func(condition batchv1.JobCondition) bool {
		return condition.Status == corev1.ConditionTrue &&
			(condition.Type == batchv1.JobComplete || condition.Type == batchv1.JobFailed)
	})

	if idx == -1 {
		return helm.ChartStateInProgress, nil
	}

	if job.Status.Conditions[idx].Type == batchv1.JobComplete {
		return helm.ChartStateSucceeded, nil
	}

	return helm.ChartStateFailed, nil
}

// aggregateResults aggregates chart upgrade results into a single PhaseStatus.
func aggregateResults(results []chartUpgradeResult, totalCharts int, chartKind string) *upgrade.PhaseStatus {
	if len(results) == 0 {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeSucceeded,
			Message: fmt.Sprintf("No %s charts to reconcile", chartKind),
		}
	}

	var failed, inProgress, succeeded, skipped int
	var failedChart string

	for _, result := range results {
		switch result.state {
		case helm.ChartStateFailed:
			failed++
			if failedChart == "" {
				failedChart = result.chartName
			}
		case helm.ChartStateInProgress:
			inProgress++
		case helm.ChartStateSucceeded, helm.ChartStateVersionAlreadyInstalled:
			succeeded++
		case helm.ChartStateNotInstalled:
			skipped++
		}
	}

	if failed > 0 {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeFailed,
			Message: fmt.Sprintf("Chart %s upgrade failed", failedChart),
		}
	}

	if inProgress > 0 {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeInProgress,
			Message: fmt.Sprintf("%s charts in progress (%d/%d completed, %d skipped)", chartKind, succeeded, totalCharts-skipped, skipped),
		}
	}

	if succeeded == 0 && skipped == totalCharts {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeSucceeded,
			Message: fmt.Sprintf("All %s charts skipped (not installed on cluster)", chartKind),
		}
	}

	return &upgrade.PhaseStatus{
		State:   lifecyclev1alpha1.UpgradeSucceeded,
		Message: fmt.Sprintf("All %d %s charts upgraded successfully (%d skipped)", succeeded, chartKind, skipped),
	}
}

// resolveHelmChartValues resolves the values of the given HelmChart based on the specified incoming configuration.
func (r *HelmReconciler) resolveHelmChartValues(chart *helmv1.HelmChart, incomingConfig *upgrade.HelmChartConfig) error {
	incomingValues, err := r.generateCustomValuesMap(incomingConfig)
	if err != nil {
		return fmt.Errorf("generating HelmChart custom values content: %w", err)
	}

	// Clear the value state. Doing this will ensure that only data provided from the Release
	// resource will be persisted.
	chart.Spec.Values = nil
	if incomingValues != nil {
		rawValues, err := json.Marshal(incomingValues)
		if err != nil {
			return fmt.Errorf("marshaling HelmChart custom values content: %w", err)
		}
		chart.Spec.Values = &apiextensionsv1.JSON{Raw: rawValues}
	}

	// Clear the value secrets state. Doing this will ensure that only data provided from the Release
	// resource will be persisted.
	chart.Spec.ValuesSecrets = nil
	if incomingSecret := incomingConfig.RuntimeConfig.ValuesFrom.SecretRef; incomingSecret != nil {
		chart.Spec.ValuesSecrets = []helmv1.SecretSpec{{Name: incomingSecret.Name, Keys: incomingSecret.Keys}}
	}

	return nil
}

// generateCustomValuesMap generates a custom chart values map from the specified chart configuration.
// Returns nil if there are no custom chart values for the chart config.
func (r *HelmReconciler) generateCustomValuesMap(config *upgrade.HelmChartConfig) (map[string]any, error) {
	// Merge custom values defined in the release manifest with values defined at runtime
	// with runtime values taking precedence.
	return helm.MergeHelmValues(config.Chart.Values, config.RuntimeConfig.Values)
}

// shouldUpdateChart determines whether the existing chart needs to be updated based on the incoming chart configuration.
func (r *HelmReconciler) shouldUpdateChart(existing *helmv1.HelmChart, incomingConfig *upgrade.HelmChartConfig) (bool, error) {
	// Mismatch between existing and incoming chart version.
	if existing.Spec.Version != incomingConfig.Chart.Version {
		return true, nil
	}

	// Construct the custom values for this chart.
	incomingValues, err := r.generateCustomValuesMap(incomingConfig)
	if err != nil {
		return false, fmt.Errorf("generating HelmChart custom values content: %w", err)
	}

	var existingValues map[string]any
	if existing.Spec.Values != nil {
		if err := json.Unmarshal(existing.Spec.Values.Raw, &existingValues); err != nil {
			return false, fmt.Errorf("unmarshaling existing HelmChart values: %w", err)
		}
	}

	// Mismatch between existing and incoming chart custom values.
	if !equality.Semantic.DeepEqual(existingValues, incomingValues) {
		return true, nil
	}

	var desiredSecrets []helmv1.SecretSpec
	if secret := incomingConfig.RuntimeConfig.ValuesFrom.SecretRef; secret != nil {
		desiredSecrets = []helmv1.SecretSpec{{Name: secret.Name, Keys: secret.Keys}}
	}

	// Mismatch between existing and incoming chart custom value secrets.
	return !slices.EqualFunc(existing.Spec.ValuesSecrets, desiredSecrets, func(a, b helmv1.SecretSpec) bool {
		return a.Name == b.Name && slices.Equal(a.Keys, b.Keys)
	}), nil
}
