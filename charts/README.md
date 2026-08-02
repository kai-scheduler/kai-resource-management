# Helm charts

Helm charts maintained by this repository live in this directory.

The primary chart is located at:

```text
charts/kai-resource-management/
```

See the [chart documentation](kai-resource-management/README.md) for
prerequisites, build and test commands, installation, upgrades, and cleanup.

The chart directory name must clearly identify the artifact as a Helm chart
through its `charts/` parent. Chart source, tests, and documentation belong
together, while developer commands remain exposed through the repository's
single root `Makefile`.

Do not add a Makefile inside an individual chart.
