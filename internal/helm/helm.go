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

package helm

import (
	"fmt"

	helmrelease "helm.sh/helm/v4/pkg/release"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	helmstorage "helm.sh/helm/v4/pkg/storage"
	helmdriver "helm.sh/helm/v4/pkg/storage/driver"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	corev1client "k8s.io/client-go/kubernetes/typed/core/v1"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

// ChartState represents the current state of a Helm chart upgrade.
type ChartState int

const (
	ChartStateUnknown ChartState = iota
	ChartStateNotInstalled
	ChartStateVersionAlreadyInstalled
	ChartStateInProgress
	ChartStateFailed
	ChartStateSucceeded
)

// String returns a human-readable representation of the chart state.
func (s ChartState) String() string {
	switch s {
	case ChartStateNotInstalled:
		return "not installed"
	case ChartStateVersionAlreadyInstalled:
		return "version already installed"
	case ChartStateInProgress:
		return "in progress"
	case ChartStateFailed:
		return "failed"
	case ChartStateSucceeded:
		return "succeeded"
	default:
		return "unknown"
	}
}

var ErrReleaseNotFound = helmdriver.ErrReleaseNotFound

// ReleaseInfo contains relevant information about a Helm release
// needed for upgrade operations.
type ReleaseInfo struct {
	ChartVersion string
	Namespace    string
	Config       map[string]any
	Revisions    int
}

// Client provides access to Helm release information.
type Client interface {
	RetrieveRelease(name string) (*ReleaseInfo, error)
	RetrieveNamespacedRelease(namespace, name string) (*ReleaseInfo, error)
}

// StorageClient implements Client using Helm storage.
type StorageClient struct {
	secrets corev1client.SecretsGetter
}

// NewClient creates a Helm storage client using the in-cluster config.
func NewClient() (*StorageClient, error) {
	cfg, err := config.GetConfig()
	if err != nil {
		return nil, fmt.Errorf("retrieving cluster config: %w", err)
	}

	k8sClient, err := kubernetes.NewForConfig(cfg)
	if err != nil {
		return nil, fmt.Errorf("creating kubernetes client: %w", err)
	}

	return &StorageClient{secrets: k8sClient.CoreV1()}, nil
}

// RetrieveRelease retrieves the latest Helm release by name, searching all namespaces.
func (c *StorageClient) RetrieveRelease(name string) (*ReleaseInfo, error) {
	return releaseInfo(c.storage(metav1.NamespaceAll).Last(name))
}

// RetrieveNamespacedRelease retrieves the latest Helm release by name, searching only the given namespace.
func (c *StorageClient) RetrieveNamespacedRelease(namespace, name string) (*ReleaseInfo, error) {
	return releaseInfo(c.storage(namespace).Last(name))
}

// storage returns a Helm storage backed by the release Secrets in the given namespace.
func (c *StorageClient) storage(namespace string) *helmstorage.Storage {
	return helmstorage.Init(helmdriver.NewSecrets(c.secrets.Secrets(namespace)))
}

// releaseInfo converts the given Helm release to a ReleaseInfo.
func releaseInfo(rel helmrelease.Releaser, err error) (*ReleaseInfo, error) {
	if err != nil {
		return nil, err
	}

	release, ok := rel.(*releasev1.Release)
	if !ok {
		return nil, fmt.Errorf("unexpected release type: %T", rel)
	}

	return &ReleaseInfo{
		ChartVersion: release.Chart.Metadata.Version,
		Namespace:    release.Namespace,
		Config:       release.Config,
		Revisions:    release.Version,
	}, nil
}
