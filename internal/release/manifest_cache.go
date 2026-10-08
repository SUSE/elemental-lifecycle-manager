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

package release

import (
	"context"
	"encoding/json"
	"fmt"

	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/suse/elemental/v3/pkg/manifest/resolver"
)

const (
	manifestCacheConfigMapName = "release-manifest-cache"
	// rawManifestKeySuffix distinguishes the raw core manifest entry from the resolved
	// manifest entry, which is stored under the bare release version.
	rawManifestKeySuffix = ".raw.yaml"
)

// ManifestCache provides ConfigMap-based caching for release manifests.
// For each release version it can hold two independent entries: the ResolvedManifest
// and the raw core manifest bytes.
type ManifestCache struct {
	client.Client
}

// Get retrieves a cached manifest for the given release version.
// Returns nil if not found in cache.
func (c *ManifestCache) Get(ctx context.Context, namespace, version string) (*resolver.ResolvedManifest, error) {
	data, found, err := c.get(ctx, namespace, version)
	if err != nil || !found {
		return nil, err
	}

	manifest := &resolver.ResolvedManifest{}
	if err := json.Unmarshal([]byte(data), manifest); err != nil {
		return nil, fmt.Errorf("unmarshaling cached manifest: %w", err)
	}

	return manifest, nil
}

// Set stores a manifest in the cache for the given release version.
func (c *ManifestCache) Set(ctx context.Context, namespace, version string, manifest *resolver.ResolvedManifest) error {
	data, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("marshaling manifest: %w", err)
	}

	return c.set(ctx, namespace, version, string(data))
}

// GetRaw retrieves the cached raw core manifest bytes for the given release version.
// Returns nil if not found in cache.
func (c *ManifestCache) GetRaw(ctx context.Context, namespace, version string) ([]byte, error) {
	data, found, err := c.get(ctx, namespace, version+rawManifestKeySuffix)
	if err != nil || !found {
		return nil, err
	}

	return []byte(data), nil
}

// SetRaw stores the raw core manifest bytes in the cache for the given release version.
func (c *ManifestCache) SetRaw(ctx context.Context, namespace, version string, data []byte) error {
	return c.set(ctx, namespace, version+rawManifestKeySuffix, string(data))
}

// get returns the value stored under key, and whether it was present.
func (c *ManifestCache) get(ctx context.Context, namespace, key string) (string, bool, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Client.Get(ctx, types.NamespacedName{
		Name:      manifestCacheConfigMapName,
		Namespace: namespace,
	}, configMap)

	if apierrors.IsNotFound(err) {
		return "", false, nil
	}
	if err != nil {
		return "", false, fmt.Errorf("getting manifest cache ConfigMap: %w", err)
	}

	value, found := configMap.Data[key]
	return value, found, nil
}

// set stores value under key, creating the ConfigMap if needed and leaving other keys untouched.
func (c *ManifestCache) set(ctx context.Context, namespace, key, value string) error {
	configMap := &corev1.ConfigMap{}
	err := c.Client.Get(ctx, types.NamespacedName{
		Name:      manifestCacheConfigMapName,
		Namespace: namespace,
	}, configMap)

	if apierrors.IsNotFound(err) {
		configMap = &corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{
				Name:      manifestCacheConfigMapName,
				Namespace: namespace,
			},
			Data: map[string]string{
				key: value,
			},
		}
		return c.Create(ctx, configMap)
	}

	if err != nil {
		return fmt.Errorf("getting manifest cache ConfigMap: %w", err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[key] = value

	return c.Update(ctx, configMap)
}
