# Quickstart

## Requirements

### System Upgrade Controller (SUC)

Elemental Lifecycle Manager (LCM) utilizes [System Upgrade Controller](https://github.com/rancher/system-upgrade-controller)
to facilitate operating system and Kubernetes upgrades on each cluster node.

SUC can be deployed in one of the following ways:

* Manually deploying the `system-upgrade-controller.yaml` file from the desired [SUC release](https://github.com/rancher/system-upgrade-controller/releases).
* Through the SUC chart located under the https://charts.rancher.io Helm repository.
* By deploying Rancher - SUC is typically included as part of the default Rancher setup.

> IMPORTANT: SUC must be deployed in the `cattle-system` namespace.

### Helm Controller

LCM facilitates additional component upgrades by using the [Helm Controller](https://github.com/k3s-io/helm-controller).

RKE2 clusters have this controller built-in. It is enabled by default and users of LCM should ensure that it is not manually
disabled via the respective CLI argument or config file parameter.

## Install Elemental Lifecycle Manager

Elemental Lifecycle Manager (LCM) can be easily installed through its OCI container images:

1. Install LCM CRDs:
    ```sh
    helm install elemental-lifecycle-manager-crds \
      oci://registry.suse.com/beta/uc/elemental-lifecycle-manager-crds \
      --namespace elemental-system \
      --create-namespace
    ```

2. Install LCM chart:
    ```sh
    helm install elemental-lifecycle-manager \
      oci://registry.suse.com/beta/uc/elemental-lifecycle-manager \
      --namespace elemental-system
    ```

For more information on chart deployment and customization, refer to the [Helm Chart Reference](docs/helm-chart-ref.md) guide.

## Trigger an Upgrade Process

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
