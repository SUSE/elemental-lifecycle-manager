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

package controller

import (
	"context"

	. "github.com/onsi/ginkgo/v2"
	. "github.com/onsi/gomega"

	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/types"
	"sigs.k8s.io/controller-runtime/pkg/reconcile"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"

	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	"github.com/suse/elemental/v3/pkg/manifest/api/core"
	"github.com/suse/elemental/v3/pkg/manifest/resolver"
)

var _ = Describe("Release Controller", func() {
	Context("When reconciling a resource", func() {
		const resourceName = "test-resource"
		var typeNamespacedName types.NamespacedName
		var defaultCtx context.Context
		var defaultManifestRetrieve func(ctx context.Context, registry, version string) (*resolver.ResolvedManifest, error)
		var defaultPipeline *upgrade.Pipeline
		var defaultReconciler *ReleaseReconciler
		var defaultRelease *lifecyclev1alpha1.Release

		release := &lifecyclev1alpha1.Release{}

		BeforeEach(func() {
			By("creating the custom resource for the Kind Release")
			defaultCtx = context.Background()
			typeNamespacedName = types.NamespacedName{
				Name:      resourceName,
				Namespace: "default",
			}

			defaultRelease = &lifecyclev1alpha1.Release{
				ObjectMeta: metav1.ObjectMeta{
					Name:      resourceName,
					Namespace: "default",
				},
				Spec: lifecyclev1alpha1.ReleaseSpec{
					Version:  "0.0.0",
					Registry: "https://foo.bar.com",
				}}

			err := k8sClient.Get(defaultCtx, typeNamespacedName, release)
			if err != nil && errors.IsNotFound(err) {
				Expect(k8sClient.Create(defaultCtx, defaultRelease)).To(Succeed())
			}

			defaultManifestRetrieve = func(_ context.Context, _, _ string) (*resolver.ResolvedManifest, error) { //nolint:unparam

				return &resolver.ResolvedManifest{
					CorePlatform: &core.ReleaseManifest{
						Metadata: &api.Metadata{},
						Components: core.Components{
							OperatingSystem: &core.OperatingSystem{},
							Kubernetes:      &core.Kubernetes{},
						},
					},
				}, nil
			}

			defaultPipeline = upgrade.NewPipeline()

			defaultReconciler = &ReleaseReconciler{
				Client:           k8sClient,
				Scheme:           k8sClient.Scheme(),
				RetrieveManifest: defaultManifestRetrieve,
				Pipeline:         defaultPipeline,
			}
		})

		AfterEach(func() {
			resource := &lifecyclev1alpha1.Release{}
			err := k8sClient.Get(defaultCtx, typeNamespacedName, resource)
			Expect(err).NotTo(HaveOccurred())

			By("Cleanup the specific resource instance Release")
			Expect(k8sClient.Delete(defaultCtx, resource)).To(Succeed())
		})
		It("should successfully reconcile the resource", func() {
			By("Reconciling the created resource")
			_, err := defaultReconciler.Reconcile(defaultCtx, reconcile.Request{
				NamespacedName: typeNamespacedName,
			})
			Expect(err).NotTo(HaveOccurred())
		})
	})
})
