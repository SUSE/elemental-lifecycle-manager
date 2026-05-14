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

package testutil

import (
	"context"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
)

// MockPlanReconciler is a mock implementation of reconcilers.PlanReconciler for testing.
// It allows test-specific behavior to be injected via function fields.
type MockPlanReconciler struct {
	ReconcileFn func(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error)
}

// Reconcile implements reconcilers.PlanReconciler interface.
// If ReconcileFn is set, it delegates to that function.
// Otherwise, returns nil result and nil error.
func (m *MockPlanReconciler) Reconcile(ctx context.Context, desired *upgradecattlev1.Plan) (*reconcilers.PlanResult, error) {
	if m.ReconcileFn != nil {
		return m.ReconcileFn(ctx, desired)
	}
	return nil, nil
}

// NewMockPlanReconciler creates a new MockPlanReconciler with default behavior.
func NewMockPlanReconciler() *MockPlanReconciler {
	return &MockPlanReconciler{}
}
