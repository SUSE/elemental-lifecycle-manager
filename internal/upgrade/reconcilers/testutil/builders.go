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
	"time"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	"github.com/suse/elemental/v3/pkg/manifest/api"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
)

type ConfigOpts func(config *upgrade.Config)

// NewTestConfig creates a basic upgrade.Config for testing.
func NewTestConfig(opts ...ConfigOpts) *upgrade.Config {
	config := &upgrade.Config{
		ReleaseNamespacedName: types.NamespacedName{
			Name:      "test-release",
			Namespace: "default",
		},
		ReleaseVersion: "v1.0.0",
	}

	for _, opt := range opts {
		opt(config)
	}
	return config
}

func WithOS(image, version string) ConfigOpts {
	return func(c *upgrade.Config) {
		c.OS = &upgrade.OSConfig{
			Image:   image,
			Version: version,
			DrainOpts: &upgrade.DrainOpts{
				ControlPlane: false,
				Worker:       false,
			},
		}
	}
}

func WithKubernetes(image, version string) ConfigOpts {
	return func(c *upgrade.Config) {
		c.Kubernetes = &upgrade.KubernetesConfig{
			Image:   image,
			Version: version,
			DrainOpts: &upgrade.DrainOpts{
				ControlPlane: false,
				Worker:       false,
			},
		}
	}
}

func WithHelmCharts(charts []*api.HelmChart) ConfigOpts {
	return func(c *upgrade.Config) {
		c.HelmCharts = &upgrade.HelmChartConfig{
			Charts:       charts,
			Repositories: []*api.HelmRepository{},
		}
	}
}

type HelmChartOpts func(h *api.HelmChart)

func WithDependencies(dependencies []string) HelmChartOpts {
	return func(h *api.HelmChart) {
		h.DependsOn = make([]api.HelmChartDependency, len(dependencies))
		for i, dep := range dependencies {
			h.DependsOn[i] = api.HelmChartDependency{
				Name: dep,
			}
		}
	}
}

// NewTestHelmChart creates a test api.HelmChart.
func NewTestHelmChart(name, version string, opts ...HelmChartOpts) *api.HelmChart {
	chart := &api.HelmChart{
		Name:    name,
		Chart:   name,
		Version: version,
		Values:  map[string]any{},
	}

	for _, opt := range opts {
		opt(chart)
	}

	return chart
}

type NodeOpts func(node *corev1.Node)

func WithVersion(version string) NodeOpts {
	return func(node *corev1.Node) {
		node.Status.NodeInfo.KubeletVersion = version
	}
}

// NewTestNode creates a test Kubernetes node.
func NewTestNode(name string, isControlPlane bool, opts ...NodeOpts) *corev1.Node {
	node := &corev1.Node{
		ObjectMeta: metav1.ObjectMeta{
			Name:   name,
			Labels: map[string]string{},
		},
		Status: corev1.NodeStatus{
			Conditions: []corev1.NodeCondition{
				{
					Type:   corev1.NodeReady,
					Status: corev1.ConditionTrue,
				},
			},
			NodeInfo: corev1.NodeSystemInfo{
				KubeletVersion: "v1.30.0",
			},
		},
	}
	if isControlPlane {
		node.Labels["node-role.kubernetes.io/control-plane"] = ""
	}

	for _, opt := range opts {
		opt(node)
	}

	return node
}

// NewTestSUCPlan creates a test System Upgrade Controller Plan.
func NewTestSUCPlan(name, namespace string) *upgradecattlev1.Plan {
	return &upgradecattlev1.Plan{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: upgradecattlev1.PlanSpec{
			NodeSelector: &metav1.LabelSelector{
				MatchLabels: map[string]string{},
			},
		},
		Status: upgradecattlev1.PlanStatus{},
	}
}

// NewTestJob creates a test Kubernetes Job.
func NewTestJob(name, namespace string, complete bool) *batchv1.Job {
	job := &batchv1.Job{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
		},
		Spec: batchv1.JobSpec{},
		Status: batchv1.JobStatus{
			Conditions: []batchv1.JobCondition{},
		},
	}
	if complete {
		now := metav1.Now()
		job.Status.Conditions = append(job.Status.Conditions, batchv1.JobCondition{
			Type:   batchv1.JobComplete,
			Status: corev1.ConditionTrue,
		})
		job.Status.CompletionTime = &now
	}
	return job
}

// NewTestJobWithCompletionTime creates a test Job with a specific completion time.
func NewTestJobWithCompletionTime(name, namespace string, completionTime time.Time) *batchv1.Job {
	job := NewTestJob(name, namespace, true)
	t := metav1.NewTime(completionTime)
	job.Status.CompletionTime = &t
	return job
}

// NewTestHelmChartCR creates a test Helm Controller HelmChart CR.
func NewTestHelmChartCR(name, namespace, version string) *helmv1.HelmChart {
	return &helmv1.HelmChart{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Annotations: map[string]string{
				"helm.cattle.io/chart-url": "https://example.com/charts",
			},
		},
		Spec: helmv1.HelmChartSpec{
			Chart:   name,
			Version: version,
		},
		Status: helmv1.HelmChartStatus{},
	}
}
