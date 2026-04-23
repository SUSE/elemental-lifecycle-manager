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

	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/plan"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
)

// OSReconciler ensures that the operating system of the cluster nodes reflects the desired release state.
type OSReconciler struct {
	client.Client
	sucReconciler PlanReconciler
}

func NewOSReconciler(c client.Client, sucReconciler PlanReconciler) *OSReconciler {
	return &OSReconciler{Client: c, sucReconciler: sucReconciler}
}

func (r *OSReconciler) Phase() upgrade.Phase {
	return upgrade.PhaseOS
}

func (r *OSReconciler) Reconcile(ctx context.Context, config *upgrade.Config) (*upgrade.PhaseStatus, error) {
	if config == nil || config.OS == nil {
		return r.Phase().SkippedStatus(), nil
	}

	logger := log.FromContext(ctx)
	osConfig := config.OS
	logger.Info("Reconciling OS upgrade",
		"image", osConfig.Image,
		"version", osConfig.Version,
		"release", config.ReleaseNamespacedName.Name)

	// prepare an ordered list of SUC Plans for the different node types of the cluster.
	plans, err := r.preparePlans(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("preparing OS upgrade plans: %w", err)
	}

	// reconcile each plan in the prepared list.
	for _, p := range plans {
		result, err := r.sucReconciler.Reconcile(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("reconciling OS upgrade plan '%s': %w", p.Name, err)
		}

		// If the plan is not in a 'Complete' status, return its current status.
		// This ensures that we do not start any reconciliation of another plan before the
		// first plan in the list has completed.
		if result.Status.State != lifecyclev1alpha1.PlanComplete {
			return result.Status, nil
		}

		logger.Info("OS upgrade plan completed",
			"plan", p.Name,
			"namespace", p.Namespace,
			"applied_on", getNodeNamesFromList(result.Nodes),
		)
	}

	return &upgrade.PhaseStatus{
		State:   lifecyclev1alpha1.UpgradeSucceeded,
		Message: "All nodes upgraded successfully",
	}, nil
}

// preparePlans determines which types of SUC Plans does the cluster need and returns a list of ordered SUC plans ready for reconciliation.
// Control plane plans are always ordered before worker plans, ensuring that all control-plane nodes are upgraded before any worker upgrade
// operation starts.
func (r *OSReconciler) preparePlans(ctx context.Context, config *upgrade.Config) ([]*upgradecattlev1.Plan, error) {
	osConfig := config.OS
	cpPlan, err := plan.OSControlPlane(config.ReleaseNamespacedName.Name, osConfig.Image, osConfig.Version, config.ReleaseVersion, osConfig.DrainOpts.ControlPlane)
	if err != nil {
		return nil, fmt.Errorf("generating OS control-plane plan: %w", err)
	}

	planList := []*upgradecattlev1.Plan{cpPlan}

	allNodes := &corev1.NodeList{}
	if err := r.List(ctx, allNodes); err != nil {
		return nil, fmt.Errorf("listing cluster nodes: %w", err)
	}

	if !isControlPlaneOnlyCluster(allNodes.Items) {
		wkPlan, err := plan.OSWorker(config.ReleaseNamespacedName.Name, osConfig.Image, osConfig.Version, config.ReleaseVersion, osConfig.DrainOpts.Worker)
		if err != nil {
			return nil, fmt.Errorf("generating OS worker plan: %w", err)
		}
		planList = append(planList, wkPlan)
	}

	return planList, nil
}

// isControlPlaneOnlyCluster returns true if all nodes in the cluster are control plane nodes.
func isControlPlaneOnlyCluster(nodes []corev1.Node) bool {
	for _, node := range nodes {
		if _, isControlPlane := node.Labels[plan.ControlPlaneLabel]; !isControlPlane {
			return false
		}
	}
	return true
}

func getNodeNamesFromList(nodes []corev1.Node) []string {
	nodeNames := make([]string, len(nodes))
	for i := range nodes {
		nodeNames[i] = nodes[i].Name
	}

	return nodeNames
}
