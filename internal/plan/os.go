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

package plan

import (
	"bytes"
	_ "embed"
	"fmt"
	"strings"
	"text/template"

	upgradecattlev1 "github.com/rancher/system-upgrade-controller/pkg/apis/upgrade.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

const (
	osControlPlaneBaseName = "elemental-os-control-plane"
	osWorkerBaseName       = "elemental-os-worker"
)

//go:embed templates/os-upgrade.sh.tpl
var osUpgradeScriptTpl string

// osControlPlaneName returns the full plan name for the given version.
func osControlPlaneName(version string) string {
	return fmt.Sprintf("%s-%s", osControlPlaneBaseName, lifecyclev1alpha1.SanitizeVersion(version))
}

// osWorkerName returns the full plan name for the given version.
func osWorkerName(version string) string {
	return fmt.Sprintf("%s-%s", osWorkerBaseName, lifecyclev1alpha1.SanitizeVersion(version))
}

// OSControlPlane builds a SUC Plan for OS upgrades on control plane nodes.
func OSControlPlane(releaseName, osImage, osVersion, releaseVersion string, drain bool) (*upgradecattlev1.Plan, error) {
	script, err := parseUpgradeScript(parseImage(osImage))
	if err != nil {
		return nil, fmt.Errorf("parsing OS upgrade script: %w", err)
	}

	p := basePlan(osControlPlaneName(osVersion), drain)
	p.Labels = map[string]string{
		lifecyclev1alpha1.ReleaseNameLabel:    releaseName,
		lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(releaseVersion),
	}
	p.Spec.Version = osVersion
	p.Spec.Concurrency = 1
	p.Spec.NodeSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      ControlPlaneLabel,
				Operator: metav1.LabelSelectorOpExists,
			},
		},
	}
	p.Spec.Upgrade = &upgradecattlev1.ContainerSpec{
		Image:   osImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
	}
	return p, nil
}

// OSWorker builds a SUC Plan for OS upgrades on worker nodes.
func OSWorker(releaseName, osImage, osVersion, releaseVersion string, drain bool) (*upgradecattlev1.Plan, error) {
	script, err := parseUpgradeScript(parseImage(osImage))
	if err != nil {
		return nil, fmt.Errorf("parsing OS upgrade script: %w", err)
	}

	p := basePlan(osWorkerName(osVersion), drain)
	p.Labels = map[string]string{
		lifecyclev1alpha1.ReleaseNameLabel:    releaseName,
		lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(releaseVersion),
	}
	p.Spec.Version = osVersion
	p.Spec.Concurrency = 1
	p.Spec.NodeSelector = &metav1.LabelSelector{
		MatchExpressions: []metav1.LabelSelectorRequirement{
			{
				Key:      ControlPlaneLabel,
				Operator: metav1.LabelSelectorOpDoesNotExist,
			},
		},
	}
	p.Spec.Upgrade = &upgradecattlev1.ContainerSpec{
		Image:   osImage,
		Command: []string{"/bin/sh", "-c"},
		Args:    []string{script},
	}
	return p, nil
}

func parseUpgradeScript(repo, version string) (string, error) {
	tmpl, err := template.New("os-upgrade").Parse(osUpgradeScriptTpl)
	if err != nil {
		return "", fmt.Errorf("allocating template for OS upgrade script: %w", err)
	}

	data := struct {
		OSImageRepo    string
		OSImageVersion string
	}{
		OSImageRepo:    repo,
		OSImageVersion: version,
	}

	var out bytes.Buffer
	if err := tmpl.Execute(&out, data); err != nil {
		return "", fmt.Errorf("applying template for OS upgrade script: %w", err)
	}

	return out.String(), nil
}

func parseImage(image string) (repo, tag string) {
	i := strings.LastIndex(image, ":")

	// Find the last slash to ensure the colon we found
	// isn't just a port number in the registry URL
	lastSlash := strings.LastIndex(image, "/")

	if i == -1 || i < lastSlash {
		return image, "latest"
	}

	return image[:i], image[i+1:]
}
