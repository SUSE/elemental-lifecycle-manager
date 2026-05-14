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
	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

// NewTestScheme creates a runtime scheme with all required types registered
// for testing reconcilers. This includes core Kubernetes types, Lifecycle Manager
// CRDs, System Upgrade Controller CRDs, and Helm Controller CRDs.
func NewTestScheme() *runtime.Scheme {
	scheme := runtime.NewScheme()
	_ = clientgoscheme.AddToScheme(scheme)
	_ = lifecyclev1alpha1.AddToScheme(scheme)
	_ = helmv1.AddToScheme(scheme)
	_ = upgradecattlev1.AddToScheme(scheme)
	return scheme
}
