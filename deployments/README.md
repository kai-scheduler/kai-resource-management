# Deployments

Kubernetes and Helm deployment assets maintained by this repository live in
this directory.

The primary chart is located at:

```text
deployments/kai-resource-management-chart/
```

See the [chart documentation](kai-resource-management-chart/README.md) for
prerequisites, build and test commands, installation, upgrades, and cleanup.

Chart source, tests, and documentation belong together, while developer
commands remain exposed through the repository's single root `Makefile`.

Do not add a Makefile inside an individual chart.
