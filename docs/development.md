# Development

This document describes the tools needed to build, test, and run Elemental Lifecycle Manager (LCM) from source.

## Prerequisites

- Go version 1.25.6+
- Podman 5.4.2+
- `kubectl` v1.35+
- Access to an environment booted from a [SUSE/elemental](https://github.com/SUSE/elemental) customized image and running RKE2 v1.35+ cluster running:
  - cert-manager 1.17+
  - Helm Controller enabled on cluster
  - System Upgrade Controller 1.19+

## Local Development

1. Ensure unit tests pass:

   ```sh
   make test
   ```

1. Build the LCM manager binary:

   ```sh
   make build
   ```

1. Install the LCM CRDs before running the controller:

   ```sh
   make install
   ```

1. Run the LCM controller locally with webhooks disabled:

   ```sh
   ENABLE_WEBHOOKS=false make run
   ```

   > NOTE: Disabling webhooks avoids requiring the Kubernetes API server to call a webhook Service backed by a controller running outside the cluster.

1. Optionally, deploy a [Release](./release-api.md) resource to trigger an [upgrade](./workflow.md) and [monitor](./monitor-and-troubleshoot.md) its progress.

## Cluster Deployment From Source

Before proceeding with the steps below, ensure that the project's unit tests pass:

```sh
make test
```

### Preparing the LCM Image

1. Prepare image build variables:

    ```sh
    # Define the image that will be built and pushed
    export IMG=<your-img-repo>/lcm:<tag>
    # Define the container tool that will be used. Defaults to `docker` if unset.
    export CONTAINER_TOOL=podman
    ```

1. Build the LCM image:

    ```sh
    make docker-build
    ```

    > NOTE: By default, `make docker-build` builds the image for the host platform. To build for a different platform, set `PLATFORM`.

1. Push the built LCM image to your registry:

    ```sh
    make docker-push
    ```

These steps build the LCM container image and push it to the registry specified by `$IMG`.

### Deploying LCM

#### Using `make`

> NOTE: Applicable only if you have access to the cluster's kubeconfig under `~/.kube/config`.

> NOTE: These steps will deploy the controller resources defined in the `config/` directory.

1. Install LCM CRDs:
  
   ```sh
   make install
   ```

1. Deploy the controller from the pre-built image:
   
   ```sh
   make deploy IMG=<your-img-repo>/lcm:<tag>
   ```

> NOTE: If you encounter RBAC errors, you may need to grant yourself cluster-admin privileges or be logged in as admin.

#### Using a Consolidated YAML File

1. Generate the consolidated LCM YAML file:

   ```sh
   make build-installer IMG=<your-img-repo>/lcm:<tag>
   ```

   > NOTE: This will produce an `install.yaml` file under the `dist/` directory.

1. Deploy the consolidated LCM YAML file:

   > NOTE: As a prerequisite step to this one, you can move the `install.yaml` file to an environment with access to the cluster.

   ```sh
   kubectl apply -f install.yaml
   ```

#### Using the LCM Helm Chart

> NOTE: Make sure the chart refers to the image that you built in the [Prepare LCM Image](#preparing-the-lcm-image) section.

Refer to the [Helm reference guide](./helm-chart-ref.md#install).


### Testing your changes

After deploying LCM with your custom changes, deploy a [Release](./release-api.md) resource to trigger an [upgrade](./workflow.md) and [monitor](./monitor-and-troubleshoot.md) its progress.

### Uninstall LCM

#### Using `make`

1. Uninstall the controller:

   ```sh
   make undeploy
   ```

1. Uninstall LCM CRDs:

   ```sh
   make uninstall
   ```

#### Using a Consolidated YAML File

```sh
kubectl delete -f install.yaml
```

#### Using the LCM Helm Chart

```sh
helm uninstall elemental-lifecycle-manager elemental-lifecycle-manager-crds --namespace elemental-system
```
