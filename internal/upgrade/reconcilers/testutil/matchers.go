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
	"github.com/onsi/gomega/types"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"

	. "github.com/onsi/gomega"
)

// HavePhaseState returns a Gomega matcher that checks if a PhaseStatus has the expected state.
func HavePhaseState(expectedState string) types.GomegaMatcher {
	return WithTransform(func(ps *upgrade.PhaseStatus) string {
		if ps == nil {
			return ""
		}
		return ps.State
	}, Equal(expectedState))
}

// HavePhaseMessage returns a Gomega matcher that checks if a PhaseStatus message equals the expected value.
func HavePhaseMessage(expectedMessage string) types.GomegaMatcher {
	return WithTransform(func(ps *upgrade.PhaseStatus) string {
		if ps == nil {
			return ""
		}
		return ps.Message
	}, Equal(expectedMessage))
}

// HavePhaseMessageContaining returns a Gomega matcher that checks if a PhaseStatus message contains a substring.
func HavePhaseMessageContaining(substring string) types.GomegaMatcher {
	return WithTransform(func(ps *upgrade.PhaseStatus) string {
		if ps == nil {
			return ""
		}
		return ps.Message
	}, ContainSubstring(substring))
}
