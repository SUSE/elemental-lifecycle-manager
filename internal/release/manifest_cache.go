/*
Copyright 2026.

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

const manifestCacheConfigMapName = "release-manifest-cache"

// ManifestCache provides ConfigMap-based caching for release manifests.
type ManifestCache struct {
	client.Client
}

// Get retrieves a cached manifest for the given release version.
// Returns nil if not found in cache.
func (c *ManifestCache) Get(ctx context.Context, namespace, version string) (*resolver.ResolvedManifest, error) {
	configMap := &corev1.ConfigMap{}
	err := c.Client.Get(ctx, types.NamespacedName{
		Name:      manifestCacheConfigMapName,
		Namespace: namespace,
	}, configMap)

	if apierrors.IsNotFound(err) {
		return nil, nil
	}
	if err != nil {
		return nil, fmt.Errorf("getting manifest cache ConfigMap: %w", err)
	}

	data, found := configMap.Data[version]
	if !found {
		return nil, nil
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

	configMap := &corev1.ConfigMap{}
	err = c.Client.Get(ctx, types.NamespacedName{
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
				version: string(data),
			},
		}
		return c.Create(ctx, configMap)
	}

	if err != nil {
		return fmt.Errorf("getting ConfigMap: %w", err)
	}

	if configMap.Data == nil {
		configMap.Data = make(map[string]string)
	}
	configMap.Data[version] = string(data)

	return c.Update(ctx, configMap)
}
