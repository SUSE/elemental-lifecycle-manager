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

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// PlanResult provides information about the current status of the SUC Plan as seen
// in the upgrade process and the nodes that this plan relates to.
type PlanResult struct {
	Status *upgrade.PhaseStatus
	Nodes  []corev1.Node
}

// PlanReconciler ensures that a specific SUC Plan reflects the desired release state.
type PlanReconciler interface {
	Reconcile(ctx context.Context, desired *upgradecattlev1.Plan) (*PlanResult, error)
}

type SUCPlanReconciler struct {
	client.Client
}

func NewSUCPlanReconciler(c client.Client) *SUCPlanReconciler {
	return &SUCPlanReconciler{Client: c}
}

// Reconcile attempts to retrieve the desired SUC plan, or create it if it is missing.
// It then provides a report on for the plan as seen in the upgrade process.
func (p *SUCPlanReconciler) Reconcile(ctx context.Context, desired *upgradecattlev1.Plan) (*PlanResult, error) {
	plan, err := p.getOrCreatePlan(ctx, desired)
	if err != nil {
		return nil, fmt.Errorf("parsing plan: %w", err)
	}

	result := &PlanResult{Status: parsePhaseStatusFromPlan(plan)}
	if result.Status.State == lifecyclev1alpha1.PlanComplete {
		nodes, err := p.listNodesForPlan(ctx, plan)
		if err != nil {
			return nil, fmt.Errorf("listing plan %q nodes: %w", plan.Name, err)
		}
		result.Nodes = nodes.Items
	}

	return result, nil
}

// getOrCreatePlan attempts to retrieve a SUC Plan, or create it if it is missing.
func (p *SUCPlanReconciler) getOrCreatePlan(ctx context.Context, desired *upgradecattlev1.Plan) (plan *upgradecattlev1.Plan, err error) {
	logger := log.FromContext(ctx)

	plan = &upgradecattlev1.Plan{}
	err = p.Get(ctx, types.NamespacedName{
		Name:      desired.Name,
		Namespace: desired.Namespace,
	}, plan)

	if apierrors.IsNotFound(err) {
		logger.Info("Creating SUC Plan", "name", desired.Name)
		if err = p.Create(ctx, desired); err != nil {
			return nil, err
		}
		return desired, nil
	}

	if err != nil {
		return nil, err
	}

	return plan, nil
}

// listNodesForPlan lists the nodes that the specified SUC Plan is responsible for.
func (p *SUCPlanReconciler) listNodesForPlan(ctx context.Context, plan *upgradecattlev1.Plan) (nodes *corev1.NodeList, err error) {
	selector, err := metav1.LabelSelectorAsSelector(plan.Spec.NodeSelector)
	if err != nil {
		return nil, fmt.Errorf("parsing node selector: %w", err)
	}

	nodes = &corev1.NodeList{}
	if err := p.List(ctx, nodes, client.MatchingLabelsSelector{Selector: selector}); err != nil {
		return nil, fmt.Errorf("listing nodes with selector %s: %w", selector, err)
	}

	return nodes, nil
}

// parsePhaseStatusFromPlan parses the SUC Plan's status to a PhaseStatus.
func parsePhaseStatusFromPlan(p *upgradecattlev1.Plan) *upgrade.PhaseStatus {
	if len(p.Status.Applying) > 0 {
		return &upgrade.PhaseStatus{
			State:   lifecyclev1alpha1.UpgradeInProgress,
			Message: fmt.Sprintf("Plan %s is currently applying on: %s", p.Name, p.Status.Applying),
		}
	}

	for _, cond := range p.Status.Conditions {
		if cond.Type == string(upgradecattlev1.PlanComplete) {
			if cond.Status == corev1.ConditionTrue {
				return &upgrade.PhaseStatus{
					State:   lifecyclev1alpha1.PlanComplete,
					Message: fmt.Sprintf("Plan %s execution completed successfully", p.Name),
				}
			}
			if cond.Status == corev1.ConditionFalse && cond.Reason != "" {
				return &upgrade.PhaseStatus{
					State:   lifecyclev1alpha1.UpgradeFailed,
					Message: fmt.Sprintf("Plan %s failed: %s", p.Name, cond.Message),
				}
			}
		}
	}

	return &upgrade.PhaseStatus{
		State:   lifecyclev1alpha1.UpgradeInProgress,
		Message: fmt.Sprintf("Plan %s execution in progress", p.Name),
	}
}
