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

// NewTestConfig creates a basic upgrade.Config for testing.
func NewTestConfig() *upgrade.Config {
	return &upgrade.Config{
		ReleaseNamespacedName: types.NamespacedName{
			Name:      "test-release",
			Namespace: "default",
		},
		ReleaseVersion: "v1.0.0",
	}
}

// NewTestConfigWithOS creates an upgrade.Config with OS configuration.
func NewTestConfigWithOS(image, version string) *upgrade.Config {
	config := NewTestConfig()
	config.OS = &upgrade.OSConfig{
		Image:   image,
		Version: version,
		DrainOpts: &upgrade.DrainOpts{
			ControlPlane: false,
			Worker:       false,
		},
	}
	return config
}

// NewTestConfigWithKubernetes creates an upgrade.Config with Kubernetes configuration.
func NewTestConfigWithKubernetes(image, version string) *upgrade.Config {
	config := NewTestConfig()
	config.Kubernetes = &upgrade.KubernetesConfig{
		Image:   image,
		Version: version,
		DrainOpts: &upgrade.DrainOpts{
			ControlPlane: false,
			Worker:       false,
		},
	}
	return config
}

// NewTestConfigWithHelmCharts creates an upgrade.Config with HelmCharts configuration.
func NewTestConfigWithHelmCharts(charts []*api.HelmChart) *upgrade.Config {
	config := NewTestConfig()
	config.HelmCharts = &upgrade.HelmChartConfig{
		Charts:       charts,
		Repositories: []*api.HelmRepository{},
	}
	return config
}

// NewTestHelmChart creates a test api.HelmChart.
func NewTestHelmChart(name, version string) *api.HelmChart {
	return &api.HelmChart{
		Name:    name,
		Chart:   name,
		Version: version,
		Values:  map[string]any{},
	}
}

// NewTestHelmChartWithDependencies creates a test api.HelmChart with dependencies.
func NewTestHelmChartWithDependencies(name, version string, dependencies []string) *api.HelmChart {
	chart := NewTestHelmChart(name, version)
	chart.DependsOn = make([]api.HelmChartDependency, len(dependencies))
	for i, dep := range dependencies {
		chart.DependsOn[i] = api.HelmChartDependency{
			Name: dep,
		}
	}
	return chart
}

// NewTestNode creates a test Kubernetes node.
func NewTestNode(name string, isControlPlane bool) *corev1.Node {
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
	return node
}

// NewTestNodeWithVersion creates a test node with a specific kubelet version.
func NewTestNodeWithVersion(name, version string, isControlPlane bool) *corev1.Node {
	node := NewTestNode(name, isControlPlane)
	node.Status.NodeInfo.KubeletVersion = version
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

// NewTestConfigMap creates a test ConfigMap.
func NewTestConfigMap(name, namespace string) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      name,
			Namespace: namespace,
			Labels:    map[string]string{},
		},
		Data: map[string]string{},
	}
}
