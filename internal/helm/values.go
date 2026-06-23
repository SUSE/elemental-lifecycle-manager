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
	"encoding/json"
	"fmt"
	"maps"

	"go.yaml.in/yaml/v3"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

// MergeHelmValues merges multiple value sources in order, with later sources taking precedence.
// Returns the merged values as a map, or nil if all sources are empty/nil.
// Accepts any combination of: string, []byte, map[string]any, or *apiextensionsv1.JSON.
func MergeHelmValues(valueSources ...any) (map[string]any, error) {
	mergedValues := map[string]any{}

	for _, v := range valueSources {
		valuesMap, err := toValuesMap(v)
		if err != nil {
			return nil, fmt.Errorf("converting to values map: %w", err)
		}

		mergedValues = mergeMaps(mergedValues, valuesMap)
	}

	if len(mergedValues) == 0 {
		return nil, nil
	}

	return mergedValues, nil
}

// toValuesMap converts a given value source to a mergable map representation.
func toValuesMap(valueSource any) (map[string]any, error) {
	valuesMap := map[string]any{}

	if valueSource == nil {
		return valuesMap, nil
	}

	switch vs := valueSource.(type) {
	case string:
		if vs != "" {
			if err := yaml.Unmarshal([]byte(vs), &valuesMap); err != nil {
				return nil, fmt.Errorf("unmarshaling chart values from string: %w", err)
			}
		}
		return valuesMap, nil
	case []byte:
		if len(vs) > 0 {
			if err := yaml.Unmarshal(vs, &valuesMap); err != nil {
				return nil, fmt.Errorf("unmarshaling chart values from byte slice: %w", err)
			}
		}
		return valuesMap, nil

	case map[string]any:
		return vs, nil

	case *apiextensionsv1.JSON:
		if vs != nil && len(vs.Raw) != 0 {
			if err := json.Unmarshal(vs.Raw, &valuesMap); err != nil {
				return nil, fmt.Errorf("unmarshaling chart values from JSON: %w", err)
			}
		}
		return valuesMap, nil
	default:
		return nil, fmt.Errorf("unexpected type %T of source values", vs)
	}
}

// mergeMaps recursively merges m2 into m1, with m2 values taking precedence.
func mergeMaps(m1, m2 map[string]any) map[string]any {
	out := make(map[string]any, len(m1))
	maps.Copy(out, m1)

	for k, v := range m2 {
		if inner, ok := v.(map[string]any); ok {
			if outInner, ok := out[k].(map[string]any); ok {
				out[k] = mergeMaps(outInner, inner)
				continue
			}
		}
		out[k] = v
	}

	return out
}
