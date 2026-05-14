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

	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade/reconcilers"
)

// MockPackagedComponentsHandler is a mock implementation of
// reconcilers.KubernetesPackagedComponentsHandler for testing.
// It allows test-specific behavior to be injected via function fields.
type MockPackagedComponentsHandler struct {
	GenerateSnapshotFn      func(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error)
	ReconcileAvailabilityFn func(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error)
}

// GenerateSnapshot implements reconcilers.KubernetesPackagedComponentsHandler interface.
// If GenerateSnapshotFn is set, it delegates to that function.
// Otherwise, returns nil snapshot and nil error.
func (m *MockPackagedComponentsHandler) GenerateSnapshot(ctx context.Context, config *upgrade.Config) (*reconcilers.PackagedComponentsSnapshot, error) {
	if m.GenerateSnapshotFn != nil {
		return m.GenerateSnapshotFn(ctx, config)
	}
	return nil, nil
}

// ReconcileAvailability implements reconcilers.KubernetesPackagedComponentsHandler interface.
// If ReconcileAvailabilityFn is set, it delegates to that function.
// Otherwise, returns nil status and nil error.
func (m *MockPackagedComponentsHandler) ReconcileAvailability(ctx context.Context, targetVersion string, snapshot *reconcilers.PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
	if m.ReconcileAvailabilityFn != nil {
		return m.ReconcileAvailabilityFn(ctx, targetVersion, snapshot)
	}
	return nil, nil
}

// NewMockPackagedComponentsHandler creates a new MockPackagedComponentsHandler with default behavior.
func NewMockPackagedComponentsHandler() *MockPackagedComponentsHandler {
	return &MockPackagedComponentsHandler{}
}
