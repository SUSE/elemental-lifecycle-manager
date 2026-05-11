# Release API

The `Release` resource is the single point of entry for environment upgrades. It defines the desired state of an environment and provides upgrade-specific configuration. Elemental Lifecycle Manager (LCM) reconciles the cluster to match the state described by this resource.


## Constraints

When defining a `Release` resource, LCM enforces the following constraints:

* Only one `Release` resource can exist on a cluster.
* Updates of an existing `Release` resource are only allowed when **no** upgrade is currently in progress.
* Downgrades of an existing `Release` resource `spec.version` field are not allowed.

## Spec

```yaml
# Release resource example
apiVersion: lifecycle.suse.com/v1alpha1
kind: Release
metadata:
  name: release-example
spec:
  version: "1.0.0"
  registry: "registry.example.org/project/release-manifest"
  disableDrain: true
```

| Field               | Description                                                              | Required |
|---------------------|--------------------------------------------------------------------------|:--------:|
| `spec.version`      | Target version of the release. Use semantic versioning, when possible.  | `true`   |
| `spec.registry`     | OCI registry from which LCM will fetch the release metadata. Metadata must be defined in the form of a [release manifest](https://github.com/SUSE/elemental/blob/main/docs/release-manifest.md).   | `true`   |
| `spec.disableDrain` | Disables node drains during upgrade phases. Defaults to `false`.        | `false`  |

## Status

| Field               | Description                                                                             |
|---------------------|-----------------------------------------------------------------------------------------|
| `status.version`    | The last release version to which the environment state was successfully upgraded.      | 
| `spec.conditions`   | Information about the state of the currently running upgrade process.                   | 
| `spec.disableDrain` | The latest resource generation observed by the controller. Meant for internal use only. |
