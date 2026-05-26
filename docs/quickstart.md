# Quickstart

Before installing LCM, make sure you have fulfilled the requirements described in the [Requirements section](./index.md#requirements).

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

For more information on chart deployment and customization, refer to the [Helm Chart Reference](./helm-chart-ref.md) guide.

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

For more details about the `Release` API, upgrade workflow, and monitoring or troubleshooting steps, see the [Guides](./index.md#guides) section.
