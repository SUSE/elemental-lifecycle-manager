# Elemental Lifecycle Manager (LCM)

Elemental Lifecycle Manager (LCM) is a Kubernetes controller that upgrades environments booted from images 
customized with the [Elemental](https://suse.github.io/elemental) toolset.

LCM can upgrade the following components:

- Operating system (SLES)
- Kubernetes distribution (RKE2)
- Additional components (Helm charts)

Users define the desired component state with a [`Release`](./release-api.md) resource, and the controller reconciles the environment until it matches that state.

> [!TIP]
> Before diving into LCM, it is essential to learn more about the Elemental toolset from its [homepage](https://suse.github.io/elemental).

## Requirements

### System Upgrade Controller (SUC)

Elemental Lifecycle Manager (LCM) utilizes [SUC](https://github.com/rancher/system-upgrade-controller) to facilitate operating system and Kubernetes upgrades on each cluster node.

SUC can be deployed in one of the following ways:

* Manually deploying the `system-upgrade-controller.yaml` file from the desired [SUC release](https://github.com/rancher/system-upgrade-controller/releases).
* Through the SUC chart located under the https://charts.rancher.io Helm repository.
* By deploying Rancher - SUC is typically included as part of the default Rancher setup.

> IMPORTANT: SUC must be deployed in the `cattle-system` namespace.

### Helm Controller

LCM facilitates additional component upgrades by using the [Helm Controller](https://github.com/k3s-io/helm-controller).

RKE2 clusters have this controller built-in. It is enabled by default and users of LCM should ensure that it is not manually
disabled via the respective CLI argument or config file parameter.

## Limitations

-  Issue: [#28](https://github.com/SUSE/elemental-lifecycle-manager/issues/28). Private Helm charts can be upgraded only when they are managed by the Helm Controller. If the chart was deployed directly through Helm (e.g. `helm install`), first create a [`HelmChart`](https://github.com/k3s-io/helm-controller/blob/master/doc/helmchart.md#HelmChart) resource with the required repository credentials before scheduling the upgrade through LCM.

-  Issue: [#28](https://github.com/SUSE/elemental-lifecycle-manager/issues/28). Helm chart upgrades that switch the chart from a public to a private reference are not handled automatically. Before scheduling the upgrade, configure the chart's corresponding [`HelmChart`](https://github.com/k3s-io/helm-controller/blob/master/doc/helmchart.md#HelmChart) resoruce to include the required credentials for the private reference.

## Guides

* [Quickstart](./quickstart.md) - For the impatient folks. :)
* [Upgrade Process Workflow](./workflow.md) - Understand how LCM performs an environment upgrade.
* [Release API](./release-api.md) - Explore the `Release` resource API, including its fields, constraints, and status reporting.
* [Helm Chart Reference](./helm-chart-ref.md) - Learn how to deploy, configure and customize the LCM Helm chart.
* [Monitoring and Troubleshooting Upgrades](./monitor-and-troubleshoot.md) - Learn how to track the progress of an upgrade and inspect the reported status.
* [Development](./development.md) - Build LCM from source, run tests, and use the local development workflow.
