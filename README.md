# Elemental Lifecycle Manager

[![Lint](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/golangci_lint.yml/badge.svg)](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/golangci_lint.yml)
[![Unit Tests](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/unit_tests.yml/badge.svg)](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/unit_tests.yml)

## Description

Elemental Lifecycle Manager (LCM) is a Kubernetes controller that upgrades environments booted from images customized with the [SUSE/elemental](https://github.com/SUSE/elemental/tree/main) toolset.


It can upgrade the following components:

* Operating system (SLES)
* Kubernetes distribution (RKE2)
* Additional components (Helm charts)

Users define the desired component state with a `Release` resource, and the controller reconciles the environment until it matches that state.

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

## Quickstart

### Install Elemental Lifecycle Manager

Elemental Lifecycle Manager (LCM) can be easily installed through its OCI container images:

1. Install LCM CRDs:
    ```sh
    helm install elemental-lifecycle-manager-crds \
      oci://registry.suse.com/elemental/elemental-lifecycle-manager-crds \
      --namespace elemental-system \
      --create-namespace
    ```

2. Install LCM chart:
    ```sh
    helm install elemental-lifecycle-manager \
      oci://registry.suse.com/elemental/elemental-lifecycle-manager \
      --namespace elemental-system
    ```

For more information on chart deployment and customization, refer to the [Helm Chart Reference](docs/helm-chart-ref.md) guide.

### Trigger an Upgrade Process

To trigger an environment upgrade process, deploy a `Release` resource in the LCM namespace.

```sh
kubectl apply -f - <<EOF
apiVersion: lifecycle.suse.com/v1alpha1
kind: Release
metadata:
  name: release-example
  namespace: elemental-system
spec:
  version: ${RELEASE_VERSION}
  registry: ${RELEASE_REGISTRY_URL}
EOF
```

Where:

- `${RELEASE_VERSION}` is the version of your [release manifest](https://github.com/SUSE/elemental/blob/main/docs/release-manifest.md).
- `${RELEASE_REGISTRY_URL}` is the registry from where LCM will retrieve this manifest version

For more details about the `Release` API, upgrade workflow, and monitoring or troubleshooting steps, see the [Guides](#guides) section.

## Guides

* [Upgrade Process Workflow](docs/workflow.md) - Understand how LCM performs an environment upgrade.
* [Release API](docs/release-api.md) - Explore the `Release` resource API, including its fields, constraints, and status reporting.
* [Helm Chart Reference](docs/helm-chart-ref.md) - Learn how to deploy, configure and customize the LCM Helm chart.
* [Monitoring and Troubleshooting Upgrades](docs/monitor-and-troubleshoot.md) - Learn how to track the progress of an upgrade and inspect the reported status.
* [Development](docs/development.md) - Build LCM from source, run tests, and use the local development workflow. 
 
## Contribution

For contributing to LCM, please create a fork of the repository and send a Pull Request (PR). A number of GitHub Actions will be triggered on the PR and they need to pass.

Before opening a Pull Request, use `make fmt` to format the code and `make lint` to execute linting steps that are configured in `/.golangci.yml` in the base directory of the repository.

Please make sure to follow these guidelines with regards to logging and error-handling:
* Avoid logging the very same error in multiple places on error-return
* Error logging must include at least one piece of detail, never a log without details
* Prefer logging in multiple lines rather than wrapping it into a single line

PRs will be reviewed by the maintainers and require two reviews without outstanding change-request to pass and become mergeable.

## License

Copyright © 2026 SUSE LLC.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    https://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
