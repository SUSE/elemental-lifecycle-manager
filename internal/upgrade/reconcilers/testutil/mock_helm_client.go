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
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
)

// MockHelmClient is a mock implementation of helm.Client for testing.
// It allows test-specific behavior to be injected via function fields.
type MockHelmClient struct {
	RetrieveReleaseFn func(name string) (*helm.ReleaseInfo, error)
}

// RetrieveRelease implements helm.Client interface.
// If RetrieveReleaseFn is set, it delegates to that function.
// Otherwise, returns ErrReleaseNotFound.
func (m *MockHelmClient) RetrieveRelease(name string) (*helm.ReleaseInfo, error) {
	if m.RetrieveReleaseFn != nil {
		return m.RetrieveReleaseFn(name)
	}
	return nil, helm.ErrReleaseNotFound
}

// NewMockHelmClient creates a new MockHelmClient with default behavior.
func NewMockHelmClient() *MockHelmClient {
	return &MockHelmClient{}
}
