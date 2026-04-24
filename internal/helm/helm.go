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

	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	helmstorage "helm.sh/helm/v4/pkg/storage"
	helmdriver "helm.sh/helm/v4/pkg/storage/driver"

	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

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
}

// StorageClient implements Client using Helm storage.
type StorageClient struct {
	*helmstorage.Storage
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

	driver := helmdriver.NewSecrets(k8sClient.CoreV1().Secrets(""))
	storage := helmstorage.Init(driver)

	return &StorageClient{storage}, nil
}

// RetrieveRelease retrieves the latest Helm release by name.
func (c *StorageClient) RetrieveRelease(name string) (*ReleaseInfo, error) {
	rel, err := c.Last(name)
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
