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
	"fmt"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	ElementalLifecycleManagerChart     = "elemental-lifecycle-manager"
	ElementalLifecycleManagerCRDsChart = "elemental-lifecycle-manager-crds"
)

type LCMReconciler struct {
	helm *HelmReconciler
}

// NewLCMReconciler creates a new LCM reconciler.
func NewLCMReconciler(c client.Client, h helm.Client) *LCMReconciler {
	return &LCMReconciler{
		helm: NewHelmReconciler(c, h),
	}
}

func (r *LCMReconciler) Phase() upgrade.Phase {
	return upgrade.PhaseLCM
}

func (r *LCMReconciler) Reconcile(ctx context.Context, config *upgrade.Config) (*upgrade.PhaseStatus, error) {
	if config == nil || config.HelmCharts == nil {
		return r.Phase().SkippedStatus(), nil
	}
	logger := log.FromContext(ctx)

	r.helm.releaseName = config.ReleaseNamespacedName.Name
	r.helm.releaseVersion = config.ReleaseVersion

	var lcmCharts []*upgrade.HelmChartConfig
	for _, chartConfig := range config.HelmCharts {
		name := chartConfig.Chart.GetName()
		if isLCMChart(name) {
			lcmCharts = append(lcmCharts, chartConfig)
		}
	}
	if len(lcmCharts) == 0 {
		return r.Phase().SkippedStatus(), nil
	}

	orderedChartConfigs, err := sortChartConfigsByDependencies(lcmCharts)
	if err != nil {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeFailed,
			Message: fmt.Sprintf("Failed to resolve chart dependencies: %v", err),
		}, err
	}

	logger.Info("Reconciling LCM charts and CRDs", "count", len(orderedChartConfigs))

	var results []chartUpgradeResult
	for _, chartConfig := range orderedChartConfigs {
		name := chartConfig.Chart.GetName()
		state, err := r.helm.reconcileChart(ctx, chartConfig)
		if err != nil {
			return &upgrade.PhaseStatus{
				State:   lifecyclev1alpha1.UpgradeFailed,
				Message: fmt.Sprintf("Failed to reconcile LCM chart %s: %v", name, err),
			}, err
		}

		results = append(results, chartUpgradeResult{
			chartName: name,
			state:     state,
		})

		if state == helm.ChartStateInProgress {
			logger.Info("LCM chart upgrade in progress, waiting", "chart", name)
			break
		}
	}

	return aggregateResults(results, len(orderedChartConfigs), "LCM"), nil
}

// isLCMChart reports whether name is one of LCM's own charts. These are upgraded by the LCM phase and skipped by the Helm chart phase
func isLCMChart(name string) bool {
	return name == ElementalLifecycleManagerCRDsChart || name == ElementalLifecycleManagerChart
}
