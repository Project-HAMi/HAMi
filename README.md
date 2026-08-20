<p align="center">
  <a href="https://project-hami.io/">
    <img src="imgs/hami-horizontal-colordark.png" alt="HAMi" width="360">
  </a>
</p>

# HAMi Helm Chart Repository

This GitHub Pages site hosts the official Helm chart repository for [HAMi](https://github.com/Project-HAMi/HAMi), a CNCF Incubating project.

> This is not the HAMi documentation website. For maintained installation guides, configuration, and project documentation, visit [project-hami.io/docs](https://project-hami.io/docs).

## Use the Helm repository

Add the repository and refresh its index:

```bash
helm repo add hami-charts https://project-hami.github.io/HAMi/
helm repo update hami-charts
```

View the available chart versions:

```bash
helm search repo hami-charts/hami --versions
```

Install HAMi:

```bash
helm install hami hami-charts/hami -n kube-system
```

For deployment prerequisites and configuration options, see the [HAMi Helm installation guide](https://project-hami.io/docs/get-started/deploy-with-helm).

## Project links

- [Official website](https://project-hami.io/)
- [Documentation](https://project-hami.io/docs)
- [GitHub repository](https://github.com/Project-HAMi/HAMi)
- [Releases](https://github.com/Project-HAMi/HAMi/releases)
- [Community](https://project-hami.io/community)

The Helm repository index is available at [`index.yaml`](index.yaml), with packaged charts stored in [`charts/`](charts/).
