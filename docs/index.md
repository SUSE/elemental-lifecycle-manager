# Elemental Lifecycle Manager (LCM)

Elemental Lifecycle Manager (LCM) is a Kubernetes controller that upgrades environments booted from images 
customized with the [Elemental](https://suse.github.io/elemental) toolset.

## Features

LCM can upgrade the following components:

- Operating system (SLES)
- Kubernetes distribution (RKE2)
- Additional components (Helm charts)

> [!TIP]
> Before diving into LCM, it is essential to learn more about the Elemental toolset from its [homepage](https://suse.github.io/elemental).

## Guides

* [Quickstart](docs/quickstart.md) - For the impatient folks. :)
* [Upgrade Process Workflow](docs/workflow.md) - Understand how LCM performs an environment upgrade.
* [Release API](docs/release-api.md) - Explore the `Release` resource API, including its fields, constraints, and status reporting.
* [Helm Chart Reference](docs/helm-chart-ref.md) - Learn how to deploy, configure and customize the LCM Helm chart.
* [Monitoring and Troubleshooting Upgrades](docs/monitor-and-troubleshoot.md) - Learn how to track the progress of an upgrade and inspect the reported status.
* [Development](docs/development.md) - Build LCM from source, run tests, and use the local development workflow.
