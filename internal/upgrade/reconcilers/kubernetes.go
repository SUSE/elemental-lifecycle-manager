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
	"strings"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/plan"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	corev1 "k8s.io/api/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PackagedComponentChartSnapshot represents a snapshot of the state
// of a single HelmChart that is a packaged component of a given Kubernetes distribution.
type PackagedComponentChartSnapshot struct {
	Name             string `json:"name"`
	Namespace        string `json:"namespace"`
	ChartURL         string `json:"chartURL"`
	ChartContentSHA  string `json:"chartContentSHA"`
	ChartVersion     string `json:"chartVersion"`
	ReleaseRevisions int    `json:"releaseRevisions"`
}

// PackagedComponentsSnapshot represents a snapshot of the state
// off all packaged components related to a Kubernetes distribution.
type PackagedComponentsSnapshot struct {
	Charts []*PackagedComponentChartSnapshot
}

type KubernetesPackagedComponentsHandler interface {
	// GenerateSnapshot creates a snapshot of the packaged components for a specific Kubernetes distribution.
	// Returns the packaged components snapshot, or an error otherwise.
	GenerateSnapshot(ctx context.Context, config *upgrade.Config) (*PackagedComponentsSnapshot, error)
	// ReconcileAvailability retrieves the currently running packaged components and compares them
	// with the component states in the given snapshot. If it finds a new or changed component, it
	// waits for that component to become available.
	ReconcileAvailability(ctx context.Context, snapshot *PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error)
}

// KubernetesReconciler ensures that the Kubernetes distribution of the cluster nodes reflects the desired release state.
type KubernetesReconciler struct {
	client.Client
	sucReconciler             PlanReconciler
	packagedComponentsHandler KubernetesPackagedComponentsHandler
}

func NewKubernetesReconciler(
	c client.Client,
	sucReconciler PlanReconciler,
	packagedComponentsHandler KubernetesPackagedComponentsHandler,
) *KubernetesReconciler {
	return &KubernetesReconciler{
		Client:                    c,
		sucReconciler:             sucReconciler,
		packagedComponentsHandler: packagedComponentsHandler,
	}
}

func (r *KubernetesReconciler) Phase() upgrade.Phase {
	return upgrade.PhaseKubernetes
}

func (r *KubernetesReconciler) Reconcile(ctx context.Context, config *upgrade.Config) (*upgrade.PhaseStatus, error) {
	if config == nil || config.Kubernetes == nil {
		return r.Phase().SkippedStatus(), nil
	}

	logger := log.FromContext(ctx)
	k8sConfig := config.Kubernetes
	logger.Info("Reconciling Kubernetes upgrade",
		"image", k8sConfig.Image,
		"version", k8sConfig.Version,
		"release", config.ReleaseNamespacedName.Name)

	// Before doing any ugrades, generate a snapshot of the current Kubernetes related packaged component states.
	snapshot, err := r.packagedComponentsHandler.GenerateSnapshot(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("generating Kubernetes packaged components snapshot: %w", err)
	}

	// Prepare an ordered list of Kubernetes SUC Plans for the different node types of the cluster.
	plans, err := r.preparePlans(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("preparing Kubernetes upgrade plans: %w", err)
	}

	// Reconcile each plan in the prepared list.
	for _, p := range plans {
		result, err := r.sucReconciler.Reconcile(ctx, p)
		if err != nil {
			return nil, fmt.Errorf("reconciling Kubernetes upgrade plan '%s': %w", p.Name, err)
		}

		// If the plan is not in a 'Complete' status, return its current status.
		// This ensures that we do not start any reconciliation of another plan before the
		// first plan in the list has completed.
		if result.Status.State != lifecyclev1alpha1.PlanComplete {
			return result.Status, nil
		}

		// Verify that the Kubernetes version of the nodes managed by this plan matches the target release version.
		if !allNodesAtKubernetesVersion(result.Nodes, k8sConfig.Version) {
			return &upgrade.PhaseStatus{
				State:   lifecyclev1alpha1.UpgradeInProgress,
				Message: fmt.Sprintf("Plan %s completed, waiting for node upgrade verification", p.Name),
			}, nil
		}

		logger.Info("Kubernetes upgrade plan completed",
			"plan", p.Name,
			"namespace", p.Namespace,
			"applied_on", getNodeNamesFromList(result.Nodes),
		)
	}

	// Once Kubernetes has been upgraded on all node types, wait for the packaged components for this
	// Kubernetes distribution to become available. This ensures that both Kubernetes and its related packaged components
	// are running before the Kubernetes upgrade is marked as 'Succeeded'. It also avoids failures where the controller tries
	// to upgrade a chart that depends on a core Kubernetes packaged component before that component is ready.
	if status, err := r.packagedComponentsHandler.ReconcileAvailability(ctx, snapshot); err != nil {
		return nil, fmt.Errorf("ensuring Kubernetes packaged components availability: %w", err)
	} else if status.State != lifecyclev1alpha1.K8sPackagedComponentsAvailable {
		return status, nil
	}

	return &upgrade.PhaseStatus{
		State:   lifecyclev1alpha1.UpgradeSucceeded,
		Message: "All nodes upgraded successfully",
	}, nil
}

// preparePlans determines which types of Kubernetes SUC Plans does the cluster need and returns a list of ordered SUC plans ready for reconciliation.
// Control plane plans are always ordered before worker plans, ensuring that all control-plane nodes are upgraded before any worker upgrade
// operation starts.
func (r *KubernetesReconciler) preparePlans(ctx context.Context, config *upgrade.Config) ([]*upgradecattlev1.Plan, error) {
	k8sConfig := config.Kubernetes
	cpPlan := plan.KubernetesControlPlane(config.ReleaseNamespacedName.Name, config.ReleaseVersion, k8sConfig.Version, k8sConfig.DrainOpts.ControlPlane)
	planList := []*upgradecattlev1.Plan{cpPlan}

	allNodes := &corev1.NodeList{}
	if err := r.List(ctx, allNodes); err != nil {
		return nil, fmt.Errorf("listing cluster nodes: %w", err)
	}

	if !isControlPlaneOnlyCluster(allNodes.Items) {
		wkPlan := plan.KubernetesWorker(config.ReleaseNamespacedName.Name, config.ReleaseVersion, k8sConfig.Version, k8sConfig.DrainOpts.Worker)
		planList = append(planList, wkPlan)
	}

	return planList, nil
}

// allNodesAtKubernetesVersion returns true if all nodes have the target Kubernetes version.
// Returns false if no nodes are provided.
// A node is considered upgraded when:
// - It is in Ready condition
// - It is not marked as unschedulable
// - Its kubelet version matches the target version
func allNodesAtKubernetesVersion(nodes []corev1.Node, targetVersion string) bool {
	if len(nodes) == 0 {
		return false
	}

	for _, node := range nodes {
		if !isNodeReady(&node) {
			return false
		}

		if node.Spec.Unschedulable {
			return false
		}

		if !kubeletVersionMatches(node.Status.NodeInfo.KubeletVersion, targetVersion) {
			return false
		}
	}

	return true
}

// isNodeReady returns true if the node has a Ready condition with status True.
func isNodeReady(node *corev1.Node) bool {
	for _, condition := range node.Status.Conditions {
		if condition.Type == corev1.NodeReady {
			return condition.Status == corev1.ConditionTrue
		}
	}
	return false
}

// kubeletVersionMatches checks if the kubelet version matches the target version.
// Handles version format differences (e.g., "v1.30.0" vs "1.30.0").
func kubeletVersionMatches(kubeletVersion, targetVersion string) bool {
	// Normalize both versions by removing 'v' prefix if present
	kubelet := strings.TrimPrefix(kubeletVersion, "v")
	target := strings.TrimPrefix(targetVersion, "v")

	return kubelet == target
}
