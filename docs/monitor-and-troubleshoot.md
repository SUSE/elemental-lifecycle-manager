# Monitoring and Troubleshooting the Upgrade Process

LCM reports the current state of an in-progress upgrade in the `status.conditions` list of the `Release` resource. Each entry represents a phase that LCM tracks to determine whether the upgrade has completed successfully.

Conditions follow the Kubernetes [condition structure](https://kubernetes.io/docs/concepts/workloads/pods/pod-condition/#structure-of-a-pod-condition) and use the `reason` field to describe their current state:

| Reason       | Meaning |
|--------------|---------|
| `Pending`    | The phase has not started yet. |
| `InProgress` | The phase is currently running. |
| `Skipped`    | The phase is not required for this upgrade. |
| `Failed`     | The phase failed. This causes the overall upgrade to fail. |
| `Succeeded`  | The phase completed successfully. |

The table below summarizes the conditions LCM reports during an upgrade:

| Condition Type       | Description |
|----------------------|-------------|
| `ManifestResolved`   | Indicates whether LCM retrieved and resolved the [release manifest](https://github.com/SUSE/elemental/blob/main/docs/release-manifest.md) for the requested version. |
| `OSUpgraded`         | Tracks the operating system upgrade phase, including the related System Upgrade Controller Plans for `control-plane` and `worker` nodes. |
| `KubernetesUpgraded` | Tracks the Kubernetes upgrade phase, including related System Upgrade Controller Plans and the availability of packaged Kubernetes components after node upgrades complete. |
| `HelmChartsUpgraded` | Tracks the Helm chart upgrade phase for additional chart components defined in the release manifest. |
| `Applied`            | Summarizes the overall upgrade state. It becomes `True` only after the manifest is resolved and all upgrade phases have either succeeded or been skipped. |

## Troubleshooting Failed Conditions

This section provides troubleshooting steps for each `Release` condition when it reports as `Failed` or stays `InProgress` longer than expected.

### Release Manifest Retrieval

Condition type: `ManifestResolved`

If this phase fails or stops progressing, inspect the following resources:

| Resource                           | Namespace     | Name                     | Description |
|------------------------------------|---------------|--------------------------|-------------|
| Manifest Cache ConfigMap           | LCM namespace | `release-manifest-cache` | Use it to confirm whether the release manifest was retrieved and cached. |
| LCM Pod                            | LCM namespace | LCM Pod name             | Inspect LCM's logs for errors while retrieving, parsing, or caching the release manifest. |

### Operating System Upgrade

Condition type: `OSUpgraded`

If this phase fails or stops progressing, inspect the following resources:

| Resource                | Namespace                 | Name        | Description |
|-------------------------|---------------------------|-------------|-------------|
| SUC Plan                | `cattle-system`           | The `OSUpgraded` condition message will indicate the exact SUC Plan name | Inspect the Plan status and events for errors. | 
| SUC Plan Job            | `cattle-system`           | `apply-<suc-plan-name>-on-<node-name>` | Inspect the Job status and events for errors. |
| SUC Plan Pod            | `cattle-system`           | `apply-<suc-plan-name>-on-<node-name>` | Inspect the Pod logs for errors from the [SUSE/elemental](https://github.com/SUSE/elemental) tool set. |
| LCM Pod                 | LCM namespace             | LCM Pod name | Inspect LCM's logs for operating system upgrade reconciliation errors. |

### Kubernetes Upgrade

Condition type: `KubernetesUpgraded`

If this phase fails or stops progressing, inspect the following resources:

| Resource                          | Namespace                 | Name        | Description | 
|-----------------------------------|---------------------------|-------------|-------------|
| SUC Plan                          | `cattle-system`           | The `KubernetesUpgraded` condition message will indicate the exact SUC Plan name | Inspect the Plan status and events for errors. |
| SUC Plan Job                      | `cattle-system`           | `apply-<suc-plan-name>-on-<node-name>` | Inspect the Job status and events for errors. |
| SUC Plan Pod                      | `cattle-system`           | `apply-<suc-plan-name>-on-<node-name>` | Inspect the Pod logs for errors from the [rancher/rke2-upgrade](https://github.com/rancher/rke2-upgrade/tree/master) tool set. |
| Kubernetes Packaged Component Pod | `kube-system`             | `helm-install-rke2-<component>` | Inspect the Pod logs for packaged component errors. |
| LCM Pod                           | LCM namespace             | LCM Pod name | Inspect LCM's logs for Kubernetes upgrade reconciliation errors. |

### Additional Helm Charts Upgrade

Condition type: `HelmChartsUpgraded`

If this phase fails or stops progressing, inspect the following resources:

| Resource                      | Namespace                 | Name        | Description | 
|-------------------------------|---------------------------|-------------|-------------|
| Helm Chart Pod                | `kube-system`             | `helm-install-<chart-name>` | Inspect the Pod logs for Helm chart errors. |
| LCM Pod                       | LCM namespace             | LCM Pod name | Inspect LCM's logs for Helm chart upgrade reconciliation errors. |
