# Helm charts

Helm charts maintained by this repository live in this directory.

The primary chart will be located at:

```text
charts/kai-resource-management/
```

The chart directory name must clearly identify the artifact as a Helm chart
through its `charts/` parent. Chart source, tests, and documentation belong
together, while developer commands remain exposed through the repository's
single root `Makefile`.

Do not add a Makefile inside an individual chart.
