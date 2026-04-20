# Elemental Lifecycle Manager

[![Lint](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/golangci_lint.yml/badge.svg)](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/golangci_lint.yml)
[![Unit Tests](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/unit_tests.yml/badge.svg)](https://github.com/suse/elemental-lifecycle-manager/actions/workflows/unit_tests.yml)

## Description
Elemental Lifecycle Manager is a Kubernetes controller capable of performing upgrades for [Elemental](https://github.com/SUSE/elemental/tree/main) created environments.

## Requirements

### System Upgrade Controller (SUC)

Elemental Lifecycle Manager utilizes [SUC](https://github.com/rancher/system-upgrade-controller)
to perform OS and Kubernetes upgrades. 

Ensure that it is installed on the cluster by either:

* Manually deploying the `system-upgrade-controller.yaml` file from the desired [SUC release](https://github.com/rancher/system-upgrade-controller/releases).
* Deploying it from the SUC chart located under the  https://charts.rancher.io Helm repository.

### Helm Controller

Additional components installed on the cluster via Helm charts are being upgraded by the
[Helm Controller](https://github.com/k3s-io/helm-controller). RKE2 clusters have this controller
built-in. It is enabled by default and users of the Elemental Lifecycle Manager should ensure that it is not manually
disabled via the respective CLI argument or config file parameter.

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
