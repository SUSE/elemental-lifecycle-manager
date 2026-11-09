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

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"
	chart "helm.sh/helm/v4/pkg/chart/v2"
	"helm.sh/helm/v4/pkg/release/common"
	releasev1 "helm.sh/helm/v4/pkg/release/v1"
	helmdriver "helm.sh/helm/v4/pkg/storage/driver"
	"k8s.io/client-go/kubernetes/fake"
)

var _ = Describe("StorageClient", func() {
	var (
		clientset *fake.Clientset
		client    *StorageClient
	)

	// store writes a Helm release Secret the same way the Helm CLI does.
	store := func(namespace, name string, revision int, chartVersion string, status common.Status) {
		GinkgoHelper()

		release := &releasev1.Release{
			Name:      name,
			Namespace: namespace,
			Version:   revision,
			Info:      &releasev1.Info{Status: status},
			Chart:     &chart.Chart{Metadata: &chart.Metadata{Name: name, Version: chartVersion}},
			Config:    map[string]any{"revision": revision},
		}

		key := fmt.Sprintf("sh.helm.release.v1.%s.v%d", name, revision)
		Expect(helmdriver.NewSecrets(clientset.CoreV1().Secrets(namespace)).Create(key, release)).To(Succeed())
	}

	BeforeEach(func() {
		clientset = fake.NewClientset()
		client = &StorageClient{secrets: clientset.CoreV1()}
	})

	Describe("RetrieveRelease", func() {
		It("should return ErrReleaseNotFound when no release exists", func() {
			_, err := client.RetrieveRelease("rancher")
			Expect(err).To(MatchError(ErrReleaseNotFound))
		})

		It("should ignore releases with a different name", func() {
			store("cattle-system", "rancher-webhook", 1, "0.8.0", common.StatusDeployed)

			_, err := client.RetrieveRelease("rancher")
			Expect(err).To(MatchError(ErrReleaseNotFound))
		})

		It("should find a release in any namespace", func() {
			store("cattle-system", "rancher", 1, "2.12.0", common.StatusDeployed)

			info, err := client.RetrieveRelease("rancher")
			Expect(err).NotTo(HaveOccurred())
			Expect(info).To(Equal(&ReleaseInfo{
				ChartVersion: "2.12.0",
				Namespace:    "cattle-system",
				Config:       map[string]any{"revision": float64(1)},
				Revisions:    1,
			}))
		})

		It("should return the latest revision", func() {
			store("cattle-system", "rancher", 1, "2.12.0", common.StatusSuperseded)
			store("cattle-system", "rancher", 2, "2.12.1", common.StatusDeployed)
			store("cattle-system", "rancher", 3, "2.13.0", common.StatusPendingUpgrade)

			info, err := client.RetrieveRelease("rancher")
			Expect(err).NotTo(HaveOccurred())
			Expect(info.ChartVersion).To(Equal("2.13.0"))
			Expect(info.Revisions).To(Equal(3))
		})
	})

	Describe("RetrieveNamespacedRelease", func() {
		It("should return ErrReleaseNotFound when no release exists", func() {
			_, err := client.RetrieveNamespacedRelease("cattle-system", "rancher")
			Expect(err).To(MatchError(ErrReleaseNotFound))
		})

		It("should ignore releases in other namespaces", func() {
			store("default", "rancher", 1, "2.12.0", common.StatusDeployed)

			_, err := client.RetrieveNamespacedRelease("cattle-system", "rancher")
			Expect(err).To(MatchError(ErrReleaseNotFound))
		})

		It("should only consider revisions from the given namespace", func() {
			store("cattle-system", "rancher", 1, "2.12.0", common.StatusDeployed)
			store("default", "rancher", 5, "2.13.0", common.StatusDeployed)

			info, err := client.RetrieveNamespacedRelease("cattle-system", "rancher")
			Expect(err).NotTo(HaveOccurred())
			Expect(info.Namespace).To(Equal("cattle-system"))
			Expect(info.ChartVersion).To(Equal("2.12.0"))
			Expect(info.Revisions).To(Equal(1))
		})
	})
})
