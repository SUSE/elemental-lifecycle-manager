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
	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	apiextensionsv1 "k8s.io/apiextensions-apiserver/pkg/apis/apiextensions/v1"
)

var (
	stringValueSource = `
  image:
    repository: nginx
    tag: "1.21"
  replicas: 3
  `

	mapValueSource = map[string]any{
		"image": map[string]any{
			"repository": "changed-repo",
			"pullPolicy": "IfNotPresent",
			"tag":        "1.22",
		},
		"service": map[string]any{
			"type": "ClusterIP",
			"port": 80,
		},
		"valueSlice": []string{"value1", "value2"},
	}

	// []byte slice value source
	byteValues = []byte(`
  replicas: 3
  resources:
    limits:
      cpu: "1"
      memory: "256Mi"
    requests:
      cpu: "100m"
      memory: "128Mi"
  valueSlice:
  - "value1"
  - "value3"
  `)

	// apiextensionsv1.JSON value source
	jsonValues = &apiextensionsv1.JSON{
		Raw: []byte(`{"image": {"tag": "1.23-changed"},"ingress": {"enabled": true,"hosts": ["example.com"]}}`),
	}

	expectedMergedValues = map[string]any{
		"image": map[string]any{
			"pullPolicy": "IfNotPresent",
			"repository": "changed-repo",
			"tag":        "1.23-changed",
		},
		"ingress": map[string]any{
			"enabled": true,
			"hosts":   []any{"example.com"},
		},
		"replicas": 3,
		"resources": map[string]any{
			"limits": map[string]any{
				"cpu":    "1",
				"memory": "256Mi",
			},
			"requests": map[string]any{
				"cpu":    "100m",
				"memory": "128Mi",
			},
		},
		"valueSlice": []any{"value1", "value3"},
		"service": map[string]any{
			"port": 80,
			"type": "ClusterIP",
		},
	}
)

var _ = Describe("Values Merge", func() {
	It("Should correctly merge multiple value sources", func() {
		var nilValueType *apiextensionsv1.JSON = nil
		result, err := MergeHelmValues(stringValueSource, "", mapValueSource, map[string]any{}, byteValues, []byte{}, jsonValues, nil, nilValueType)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(Equal(expectedMergedValues))
	})

	It("Should return nil if all value sources are empty", func() {
		var nilValueType *apiextensionsv1.JSON = nil
		result, err := MergeHelmValues("", map[string]any{}, []byte{}, &apiextensionsv1.JSON{}, nil, nilValueType)
		Expect(err).ToNot(HaveOccurred())
		Expect(result).To(BeNil())
	})

	It("Should fail for unsupported source types", func() {
		var unsupportedType int
		_, err := MergeHelmValues(stringValueSource, mapValueSource, byteValues, jsonValues, unsupportedType)
		Expect(err.Error()).To(ContainSubstring("unexpected type int of source values"))
	})
})
