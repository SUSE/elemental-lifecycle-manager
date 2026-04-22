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

package upgrade

import (
	"context"
	"fmt"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
)

// PhaseHandler defines the interface for handling a single upgrade phase in the pipeline.
type PhaseHandler interface {
	// Phase returns the phase this handler is responsible for.
	Phase() Phase
	// Reconcile performs the reconciliation for this phase and returns the status.
	Reconcile(ctx context.Context, config *Config) (*PhaseStatus, error)
}

// Pipeline executes upgrade phases in sequence, stopping when a phase
// is not yet complete. This allows the controller to resume from where
// it left off on the next reconciliation.
type Pipeline struct {
	handlers []PhaseHandler
}

// NewPipeline creates a new pipeline with the given handlers.
// Handlers are executed in the order they are provided.
func NewPipeline(handlers ...PhaseHandler) *Pipeline {
	return &Pipeline{
		handlers: handlers,
	}
}

// Reconcile executes each phase handler in sequence.
// It stops and returns when:
// - A phase returns an error (wrapped in PhaseError)
// - A phase has not yet succeeded (allowing retry on next reconcile)
// - All phases complete successfully - a completed phase is a phase with a
// status marked as either 'Succeeded' or 'Skipped'
func (p *Pipeline) Reconcile(ctx context.Context, config *Config) (*Result, error) {
	result := &Result{
		PhaseStates: make(map[Phase]*PhaseStatus),
	}

	if config == nil {
		return result, fmt.Errorf("upgrade config is nil")
	}

	for _, handler := range p.handlers {
		status, err := handler.Reconcile(ctx, config)
		if err != nil {
			return result, &PhaseError{Phase: handler.Phase(), Err: err}
		}
		result.PhaseStates[handler.Phase()] = status

		// Stop if phase state is other than succeeded or skipped
		if status.State != lifecyclev1alpha1.UpgradeSucceeded && status.State != lifecyclev1alpha1.UpgradeSkipped {
			return result, nil
		}
	}

	return result, nil
}

func (p *Pipeline) Phases() []Phase {
	phases := make([]Phase, 0, len(p.handlers))
	for _, h := range p.handlers {
		phases = append(phases, h.Phase())
	}
	return phases
}
