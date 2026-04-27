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
	"errors"
	"fmt"
	"maps"
	"slices"

	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
	"sigs.k8s.io/yaml"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental/v3/pkg/manifest/api"
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
	// repositoryURLs caches repository name to URL mappings
	repositoryURLs map[string]string
	// releaseName is the name of the Release resource managing these charts
	releaseName string
	// releaseVersion is the target release version
	releaseVersion string
}

// NewHelmReconciler creates a new Helm reconciler.
func NewHelmReconciler(c client.Client, h helm.Client) *HelmReconciler {
	return &HelmReconciler{
		Client:         c,
		helmClient:     h,
		repositoryURLs: make(map[string]string),
	}
}

func (r *HelmReconciler) Phase() upgrade.Phase {
	return upgrade.PhaseHelmCharts
}

func (r *HelmReconciler) Reconcile(ctx context.Context, config *upgrade.Config) (*upgrade.PhaseStatus, error) {
	if config == nil || config.HelmCharts == nil || len(config.HelmCharts.Charts) == 0 {
		return r.Phase().SkippedStatus(), nil
	}

	return r.reconcileHelmCharts(ctx, config.ReleaseNamespacedName.Name, config.ReleaseVersion, config.HelmCharts)
}

// reconcileHelmCharts ensures the HelmChart resources exist and are up to date.
// Only charts that are already installed on the cluster will be upgraded.
// Charts are processed in dependency order.
func (r *HelmReconciler) reconcileHelmCharts(ctx context.Context, releaseName, releaseVersion string, config *upgrade.HelmChartConfig) (*upgrade.PhaseStatus, error) {
	logger := log.FromContext(ctx)

	// Store release context for labeling HelmChart resources
	r.releaseName = releaseName
	r.releaseVersion = releaseVersion

	// Build repository URL map for quick lookup
	r.repositoryURLs = make(map[string]string)
	for _, repo := range config.Repositories {
		r.repositoryURLs[repo.Name] = repo.URL
	}

	orderedCharts, err := sortChartsByDependencies(config.Charts)
	if err != nil {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeFailed,
			Message: fmt.Sprintf("Failed to resolve chart dependencies: %v", err),
		}, err
	}

	logger.Info("Reconciling Helm charts", "count", len(orderedCharts))

	var results []chartUpgradeResult
	for _, chart := range orderedCharts {
		state, err := r.reconcileChart(ctx, chart)
		if err != nil {
			return &upgrade.PhaseStatus{
				State:   lifecyclev1alpha1.UpgradeFailed,
				Message: fmt.Sprintf("Failed to reconcile chart %s: %v", chart.GetName(), err),
			}, err
		}

		results = append(results, chartUpgradeResult{
			chartName: chart.GetName(),
			state:     state,
		})

		// If a chart is in progress, we need to wait before processing dependents
		if state == helm.ChartStateInProgress {
			logger.Info("Chart upgrade in progress, waiting", "chart", chart.GetName())
			break
		}
	}

	return r.aggregateResults(results, len(orderedCharts)), nil
}

// sortChartsByDependencies returns charts sorted so that dependencies come before dependents.
func sortChartsByDependencies(charts []*api.HelmChart) ([]*api.HelmChart, error) {
	chartMap := make(map[string]*api.HelmChart)
	for _, chart := range charts {
		chartMap[chart.GetName()] = chart
	}

	// Track visited and in-progress for cycle detection
	visited, inProgress := make(map[string]bool), make(map[string]bool)
	var result []*api.HelmChart

	var visit func(name string) error
	visit = func(name string) error {
		if inProgress[name] {
			return fmt.Errorf("circular dependency detected involving chart %s", name)
		}
		if visited[name] {
			return nil
		}

		chart, exists := chartMap[name]
		if !exists {
			// Chart not in our list, skip (it might be an external dependency)
			return nil
		}

		inProgress[name] = true

		// Visit dependencies first
		for _, dep := range chart.DependsOn {
			if err := visit(dep.Name); err != nil {
				return err
			}
		}

		inProgress[name] = false
		visited[name] = true
		result = append(result, chart)

		return nil
	}

	for _, chart := range charts {
		if err := visit(chart.GetName()); err != nil {
			return nil, err
		}
	}

	return result, nil
}

// reconcileChart reconciles a single chart and returns its state.
// If the chart is not installed on the cluster, it is skipped.
func (r *HelmReconciler) reconcileChart(ctx context.Context, chart *api.HelmChart) (helm.ChartState, error) {
	logger := log.FromContext(ctx)
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
		return helm.ChartStateInProgress, r.createHelmChartFromRelease(ctx, chart, helmRelease)
	}

	if err != nil {
		return helm.ChartStateUnknown, fmt.Errorf("getting HelmChart: %w", err)
	}

	// Update existing HelmChart CR if version differs
	if existing.Spec.Version != chart.Version {
		logger.Info("Updating HelmChart for upgrade", "chart", chartName,
			"currentVersion", existing.Spec.Version,
			"targetVersion", chart.Version)
		return helm.ChartStateInProgress, r.updateHelmChart(ctx, chart, existing)
	}

	// HelmChart exists with target version, check job status
	return r.evaluateHelmChartJobStatus(ctx, existing)
}

// createHelmChartFromRelease creates a HelmChart CR from an existing Helm release.
func (r *HelmReconciler) createHelmChartFromRelease(ctx context.Context, chart *api.HelmChart, release *helm.ReleaseInfo) error {
	helmChart, err := r.buildHelmChart(chart, release.Namespace)
	if err != nil {
		return fmt.Errorf("building HelmChart: %w", err)
	}

	// Merge values from installed release with manifest values
	if len(release.Config) > 0 {
		mergedValues := mergeMaps(release.Config, chart.Values)
		valuesYAML, err := yaml.Marshal(mergedValues)
		if err != nil {
			return fmt.Errorf("marshaling merged values: %w", err)
		}
		helmChart.Spec.ValuesContent = string(valuesYAML)
	}

	return r.Create(ctx, helmChart)
}

// updateHelmChart updates an existing HelmChart CR to trigger an upgrade.
func (r *HelmReconciler) updateHelmChart(ctx context.Context, chart *api.HelmChart, existing *helmv1.HelmChart) error {
	repoURL := r.resolveRepositoryURL(chart)

	// Ensure labels are set for Release tracking
	if existing.Labels == nil {
		existing.Labels = make(map[string]string)
	}
	existing.Labels[lifecyclev1alpha1.ReleaseNameLabel] = r.releaseName
	existing.Labels[lifecyclev1alpha1.ReleaseVersionLabel] = lifecyclev1alpha1.SanitizeVersion(r.releaseVersion)

	existing.Spec.Chart = chart.Chart
	existing.Spec.Version = chart.Version
	existing.Spec.Repo = repoURL

	// Merge existing values with new manifest values
	if len(chart.Values) > 0 {
		var existingValues map[string]any
		if existing.Spec.ValuesContent != "" {
			if err := yaml.Unmarshal([]byte(existing.Spec.ValuesContent), &existingValues); err != nil {
				return fmt.Errorf("unmarshaling existing values: %w", err)
			}
		}

		mergedValues := mergeMaps(existingValues, chart.Values)
		valuesYAML, err := yaml.Marshal(mergedValues)
		if err != nil {
			return fmt.Errorf("marshaling merged values: %w", err)
		}
		existing.Spec.ValuesContent = string(valuesYAML)
	}

	return r.Update(ctx, existing)
}

// buildHelmChart creates a HelmChart resource from the manifest chart definition.
func (r *HelmReconciler) buildHelmChart(chart *api.HelmChart, targetNamespace string) (*helmv1.HelmChart, error) {
	name := chart.GetName()
	repoURL := r.resolveRepositoryURL(chart)

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
			Chart:           chart.Chart,
			Version:         chart.Version,
			Repo:            repoURL,
			TargetNamespace: targetNamespace,
			BackOffLimit:    &backoffLimit,
		},
	}

	if len(chart.Values) > 0 {
		valuesYAML, err := yaml.Marshal(chart.Values)
		if err != nil {
			return nil, fmt.Errorf("marshaling values: %w", err)
		}
		helmChart.Spec.ValuesContent = string(valuesYAML)
	}

	return helmChart, nil
}

// resolveRepositoryURL resolves the repository URL for a chart.
func (r *HelmReconciler) resolveRepositoryURL(chart *api.HelmChart) string {
	repoName := chart.GetRepositoryName()
	if repoName != "" {
		if url, ok := r.repositoryURLs[repoName]; ok {
			return url
		}
	}

	if chart.Repository != "" {
		return chart.Repository
	}

	return ""
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
func (r *HelmReconciler) aggregateResults(results []chartUpgradeResult, totalCharts int) *upgrade.PhaseStatus {
	if len(results) == 0 {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeSucceeded,
			Message: "No Helm charts to reconcile",
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
			Message: fmt.Sprintf("Helm charts in progress (%d/%d completed, %d skipped)", succeeded, totalCharts-skipped, skipped),
		}
	}

	if succeeded == 0 && skipped == totalCharts {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeSucceeded,
			Message: "All Helm charts skipped (not installed on cluster)",
		}
	}

	return &upgrade.PhaseStatus{
		State:   lifecyclev1alpha1.UpgradeSucceeded,
		Message: fmt.Sprintf("All %d Helm charts upgraded successfully (%d skipped)", succeeded, skipped),
	}
}

// mergeMaps recursively merges m2 into m1, with m2 values taking precedence.
func mergeMaps(m1, m2 map[string]any) map[string]any {
	out := make(map[string]any, len(m1))
	maps.Copy(out, m1)

	for k, v := range m2 {
		if inner, ok := v.(map[string]any); ok {
			if outInner, ok := out[k].(map[string]any); ok {
				out[k] = mergeMaps(outInner, inner)
				continue
			}
		}
		out[k] = v
	}

	return out
}
