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

package reconcilers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	helmv1 "github.com/k3s-io/helm-controller/pkg/apis/helm.cattle.io/v1"
	lifecyclev1alpha1 "github.com/suse/elemental-lifecycle-manager/api/v1alpha1"
	"github.com/suse/elemental-lifecycle-manager/internal/helm"
	"github.com/suse/elemental-lifecycle-manager/internal/upgrade"
	batchv1 "k8s.io/api/batch/v1"
	corev1 "k8s.io/api/core/v1"
	apierrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/wait"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

const (
	// chartURLAnnotation an annotation that all RKE2 packaged HelmChart components have.
	chartURLAnnotation = "helm.cattle.io/chart-url"
)

// chartSnapshotPair represents a snapshotted HelmChart component and its currently running counterpart.
type chartSnapshotPair struct {
	Chart    *helmv1.HelmChart
	Snapshot *PackagedComponentChartSnapshot
}

// RKE2PackagedComponentsHandler is responsible for ensuring the availability of packaged components related
// to the RKE2 Kubernetes distribution.
type RKE2PackagedComponentsHandler struct {
	client.Client
	helmClient     helm.Client
	findComponents func(ctx context.Context, client client.Client) (map[string]helmv1.HelmChart, error)
}

func NewRKE2PackagedComponentsHandler(
	c client.Client,
	helmClient helm.Client,
	findComponents func(ctx context.Context, c client.Client) (map[string]helmv1.HelmChart, error),
) *RKE2PackagedComponentsHandler {
	handler := &RKE2PackagedComponentsHandler{
		Client:         c,
		helmClient:     helmClient,
		findComponents: findComponents,
	}

	if handler.findComponents == nil {
		handler.findComponents = findRKE2PackagedComponents
	}

	return handler
}

// GenerateSnapshot generates a snapshot of the RKE2 packaged components based on the provided configuration.
// If a snapshot based on the desired configuration already exists, the function returns that snapshot.
// If a snapshot that is based on an older configuration exists, the function recreates the snapshot and returns the recreated version of it.
func (h *RKE2PackagedComponentsHandler) GenerateSnapshot(ctx context.Context, config *upgrade.Config) (*PackagedComponentsSnapshot, error) {
	snapshot := h.blankSnapshot(config)
	err := h.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, snapshot)
	if err != nil {
		if !apierrors.IsNotFound(err) {
			return nil, fmt.Errorf("getting RKE2 packaged component snapshot: %w", err)
		}

		// Ensure that the blank snapshot was not modified by the Get function.
		snapshot = h.blankSnapshot(config)
		if err := h.createSnapshot(ctx, snapshot, config); err != nil {
			return nil, fmt.Errorf("creating snapshot %s: %w", snapshot.Name, err)
		}
	}

	var recreate bool
	if release, ok := snapshot.Labels[lifecyclev1alpha1.ReleaseNameLabel]; !ok || release != config.ReleaseNamespacedName.Name {
		recreate = true
	}

	if version, ok := snapshot.Labels[lifecyclev1alpha1.ReleaseVersionLabel]; !ok || version != lifecyclev1alpha1.SanitizeVersion(config.ReleaseVersion) {
		recreate = true
	}

	// If the retrieved snapshot was for a different release, then recreate it.
	if recreate {
		// Ensure the snapshot is removed before creating it for the desired configuration.
		if err := h.deleteSnapshotAndWait(ctx, snapshot, 1*time.Second, 120*time.Second); err != nil {
			return nil, fmt.Errorf("waiting for snapshot %s deletion: %w", snapshot.Name, err)
		}

		snapshot = h.blankSnapshot(config)
		if err := h.createSnapshot(ctx, snapshot, config); err != nil {
			return nil, fmt.Errorf("creating snapshot %s: %w", snapshot.Name, err)
		}
	}

	return h.parseSnapshot(snapshot)
}

// ReconcileAvailability uses the provided snapshot to identify changed or newly added RKE2 HelmChart packaged components and wait for them to become available.
func (h *RKE2PackagedComponentsHandler) ReconcileAvailability(ctx context.Context, snapshot *PackagedComponentsSnapshot) (*upgrade.PhaseStatus, error) {
	snapshotPairs, err := h.findNewOrChangedRKE2PackagedComponents(ctx, snapshot.Charts)
	if err != nil {
		return nil, fmt.Errorf("finding new or changed RKE2 packaged components: %w", err)
	}

	// For each snapshot pair, wait for the currently active chart to become available.
	for _, pair := range snapshotPairs {
		// RKE2 uses the Helm Controller to manage its helm charts, as such it is enough to mark a HelmChart
		// as available if its Helm Controller created Job has completed.
		jobComplete, err := h.isHelmChartJobComplete(ctx, pair)
		if err != nil {
			return nil, fmt.Errorf("validating job for RKE2 packaged component %q: %w", pair.Chart.Name, err)
		}

		if !jobComplete {
			return &upgrade.PhaseStatus{
				State:   lifecyclev1alpha1.UpgradeInProgress,
				Message: fmt.Sprintf("RKE2 packaged component %q execution is still in progress", pair.Chart.Name),
			}, nil
		}
	}

	return &upgrade.PhaseStatus{
		State:   lifecyclev1alpha1.K8sPackagedComponentsAvailable,
		Message: "All RKE2 packaged components available",
	}, nil
}

func (h *RKE2PackagedComponentsHandler) blankSnapshot(config *upgrade.Config) *corev1.ConfigMap {
	return &corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "rke2-packaged-components-snapshot",
			Namespace: config.ReleaseNamespacedName.Namespace,
			Labels: map[string]string{
				lifecyclev1alpha1.ReleaseNameLabel:    config.ReleaseNamespacedName.Name,
				lifecyclev1alpha1.ReleaseVersionLabel: lifecyclev1alpha1.SanitizeVersion(config.ReleaseVersion),
			},
		},
	}
}

func (h *RKE2PackagedComponentsHandler) createSnapshot(ctx context.Context, snapshot *corev1.ConfigMap, config *upgrade.Config) error {
	if err := h.populateSnapshot(ctx, snapshot); err != nil {
		return fmt.Errorf("populating RKE2 packaged components snapshot: %w", err)
	}

	log.FromContext(ctx).Info("Generating RKE2 packaged components snapshot",
		"name", snapshot.Name,
		"namespace", snapshot.Namespace,
		"owner", config.ReleaseNamespacedName.Name,
	)
	return h.Create(ctx, snapshot)
}

// populateSnapshot locates the currently running RKE2 packaged components, marshals them and stores them in the
// given snapshot reference.
func (h *RKE2PackagedComponentsHandler) populateSnapshot(ctx context.Context, snapshot *corev1.ConfigMap) error {
	rke2Charts, err := h.findComponents(ctx, h.Client)
	if err != nil {
		return fmt.Errorf("retrieving RKE2 packaged components: %w", err)
	}

	if snapshot.Data == nil {
		snapshot.Data = map[string]string{}
	}

	for _, rke2Chart := range rke2Charts {
		chartSnapshot, err := h.createHelmChartSnapshot(rke2Chart)
		if err != nil {
			return fmt.Errorf("parsing fingerprint from HelmChart %q: %w", rke2Chart.Name, err)
		}
		data, err := json.Marshal(chartSnapshot)
		if err != nil {
			return fmt.Errorf("marshaling RKE2 HelmChart %q fingerprint: %w", rke2Chart.Name, err)
		}

		snapshot.Data[rke2Chart.Name] = string(data)
	}

	return nil
}

func (h *RKE2PackagedComponentsHandler) createHelmChartSnapshot(chart helmv1.HelmChart) (*PackagedComponentChartSnapshot, error) {
	snapshot := &PackagedComponentChartSnapshot{
		Name:      chart.Name,
		Namespace: chart.Namespace,
	}

	if val, ok := chart.Annotations[chartURLAnnotation]; ok {
		snapshot.ChartURL = val
	}

	if chart.Spec.ChartContent != "" {
		snapshot.ChartContentSHA = hashContent(chart.Spec.ChartContent)
	}

	info, err := h.helmClient.RetrieveRelease(chart.Name)
	if err != nil {
		return nil, fmt.Errorf("retrieving Helm release %q: %w", chart.Name, err)
	}

	snapshot.ReleaseRevisions = info.Revisions
	snapshot.ChartVersion = info.ChartVersion

	return snapshot, nil
}

func (h *RKE2PackagedComponentsHandler) parseSnapshot(snapshot *corev1.ConfigMap) (*PackagedComponentsSnapshot, error) {
	parsedSnapshot := &PackagedComponentsSnapshot{}
	for name, data := range snapshot.Data {
		parsedChartSnapshot := &PackagedComponentChartSnapshot{}
		if err := json.Unmarshal([]byte(data), parsedChartSnapshot); err != nil {
			return nil, fmt.Errorf("parsing RKE2 packaged component %q: %w", name, err)
		}
		parsedSnapshot.Charts = append(parsedSnapshot.Charts, parsedChartSnapshot)
	}

	return parsedSnapshot, nil
}

func (h *RKE2PackagedComponentsHandler) deleteSnapshotAndWait(ctx context.Context, snapshot *corev1.ConfigMap, interval time.Duration, timeout time.Duration) error {
	// Only delete if snapshot is not already being deleted
	if snapshot.GetDeletionTimestamp().IsZero() {
		if err := h.Delete(ctx, snapshot); err != nil && !apierrors.IsNotFound(err) {
			return fmt.Errorf("deleting RKE2 packaged component snapshot %q: %w", snapshot.Name, err)
		}
	}

	return wait.PollUntilContextTimeout(ctx, interval, timeout, true, func(ctx context.Context) (bool, error) {
		deleted := &corev1.ConfigMap{}
		err := h.Get(ctx, types.NamespacedName{Name: snapshot.Name, Namespace: snapshot.Namespace}, deleted)
		if apierrors.IsNotFound(err) {
			return true, nil
		}
		if err != nil {
			return false, err
		}

		return false, nil
	})
}

// findNewOrChangedRKE2PackagedComponents locates the currently running RKE2 HelmChart components and compares them against their last recorded state.
// A changed RKE2 HelmChart component is a component whose chart content or chart-url annotation differ from their last recorded state.
// A new RKE2 HelmChart component is a component who was missing from the last recorded RKE2 packaged components state.
func (h *RKE2PackagedComponentsHandler) findNewOrChangedRKE2PackagedComponents(ctx context.Context, chartSnapshots []*PackagedComponentChartSnapshot) ([]*chartSnapshotPair, error) {
	snapshotMap := make(map[string]*PackagedComponentChartSnapshot, len(chartSnapshots))
	for _, chart := range chartSnapshots {
		snapshotMap[chart.Name] = chart
	}

	latestComponents, err := h.findComponents(ctx, h.Client)
	if err != nil {
		return nil, fmt.Errorf("finding RKE2 packaged components: %w", err)
	}

	changedState := []*chartSnapshotPair{}
	newComponents := []*chartSnapshotPair{}
	for name, chart := range latestComponents {
		snap, ok := snapshotMap[name]
		if !ok {
			// Missing in snapshot => new component
			newComponents = append(newComponents, &chartSnapshotPair{Chart: &chart})
			continue
		}

		if h.chartStateChanged(&chart, snap) {
			changedState = append(changedState, &chartSnapshotPair{
				Chart:    &chart,
				Snapshot: snap,
			})
		}
	}

	return append(changedState, newComponents...), nil
}

// chartStateChanged verifies whether the given HelmChart state has changed in comparison to its snapshotted state.
func (h *RKE2PackagedComponentsHandler) chartStateChanged(chart *helmv1.HelmChart, chartSnapshot *PackagedComponentChartSnapshot) bool {
	activeContent := hashContent(chart.Spec.ChartContent)

	// In RKE2 all HelmChart component data is provided through the 'chartContent' field. In addition the 'chart-url' annotation is changed as well.
	// More details on who RKE2 constructs its resources here - https://github.com/rancher/rke2/blob/master/charts/build-chart.sh
	return activeContent != chartSnapshot.ChartContentSHA ||
		chart.Annotations[chartURLAnnotation] != chartSnapshot.ChartURL
}

func (h *RKE2PackagedComponentsHandler) isHelmChartJobComplete(ctx context.Context, pair *chartSnapshotPair) (bool, error) {
	chart := pair.Chart

	// Missing Job name in chart status means that the Helm Controller
	// has not yet scheduled a Job for the chart.
	if chart.Status.JobName == "" {
		return false, nil
	}

	job := &batchv1.Job{}
	if err := h.Get(ctx, types.NamespacedName{
		Name:      chart.Status.JobName,
		Namespace: chart.Namespace,
	}, job); err != nil {
		// Job might be cleaned up after completion, check actual helm release version
		if apierrors.IsNotFound(err) {
			return h.hasHelmReleaseAdvancedPastSnapshot(pair)
		}
		return false, err
	}

	// A completed Job for changed HelmChart resources means that the Helm Controller has successfully upgraded the chart.
	// A completed Job for new HelmChart resources means that the Helm Controller has successfully installed the chart.
	isComplete := slices.ContainsFunc(job.Status.Conditions, func(c batchv1.JobCondition) bool {
		return c.Type == batchv1.JobComplete && c.Status == corev1.ConditionTrue
	})

	if !isComplete {
		return false, nil
	}

	return h.hasHelmReleaseAdvancedPastSnapshot(pair)
}

func (h *RKE2PackagedComponentsHandler) hasHelmReleaseAdvancedPastSnapshot(pair *chartSnapshotPair) (bool, error) {
	release, err := h.helmClient.RetrieveRelease(pair.Chart.Name)
	if err != nil {
		return false, fmt.Errorf("retrieving RKE2 packaged component Helm release %q: %w", pair.Chart.Name, err)
	}

	if pair.Snapshot != nil {
		return release.ChartVersion != pair.Snapshot.ChartVersion || release.Revisions > pair.Snapshot.ReleaseRevisions, nil
	}

	// New components do not have snapshots, as such assume that an available release is enough to consider the release
	// advanced past the snapshot
	return true, nil
}

func hashContent(content string) string {
	sum := sha256.Sum256([]byte(content))
	return hex.EncodeToString(sum[:])
}

// findRKE2PackagedComponents locates all running RKE2 packaged components.
// RKE2 ships its core packaged components as HelmChart resources, so this function returns a map
// of HelmChart resources keyed by the HelmChart name.
func findRKE2PackagedComponents(ctx context.Context, c client.Client) (map[string]helmv1.HelmChart, error) {
	// Right now there is no conclusive way to retrieve a RKE2 packaged component by a single delimiter, so this function
	// uses a set of delimiters that are specific to RKE2 HelmChart resources. While it is possible to miss a some charts,
	// this set of delimiters matches all HelmCharts related to the core use-case deployment of RKE2.
	const (
		// rke2HelmChartNS is the namespace under which all RKE2 related HelmChart resources are deployed.
		rke2HelmChartNS = "kube-system"
		// namePrefix is the prefix that the core RKE2 related HelmChart resources have.
		namePrefix = "rke2-"
		// ownerAnnotation is the label for the name of the owner of the HelmChart resource.
		ownerAnnotation = "objectset.rio.cattle.io/owner-name"
		// ownerGVKAnnotation is the Group/Version/Kind label for the owner of the HelmChart resource.
		ownerGVKAnnotation = "objectset.rio.cattle.io/owner-gvk"
		// addonOwner is the actual Group/Version/Kind of the owner. All RKE2 HelmChart resources are created from
		// Addons provided under '/var/lib/rancher/rke2/server/manifests' (https://docs.rke2.io/install/packaged_components#packaged-components)
		addonOwner = "k3s.cattle.io/v1, Kind=Addon"
	)

	var allCharts helmv1.HelmChartList
	if err := c.List(ctx, &allCharts, client.InNamespace(rke2HelmChartNS)); err != nil {
		return nil, fmt.Errorf("listing HelmChart resources in %q namespace: %w", rke2HelmChartNS, err)
	}

	found := make(map[string]helmv1.HelmChart, len(allCharts.Items))
	for _, chart := range allCharts.Items {
		if !strings.HasPrefix(chart.Name, namePrefix) {
			continue
		}

		annotations := chart.Annotations

		if _, ok := annotations[ownerAnnotation]; !ok {
			continue
		}

		if val, ok := annotations[ownerGVKAnnotation]; !ok || val != addonOwner {
			continue
		}

		if _, ok := annotations[chartURLAnnotation]; !ok {
			continue
		}

		found[chart.Name] = chart
	}

	return found, nil
}
