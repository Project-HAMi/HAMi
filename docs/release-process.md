This document documents the regular release process, including image building, chart package building, artifact publishing, changelog writing, etc.

1. Update Changelog

Currently, there is no automated way to generate the changelog. The changelog file needs to be updated manually. Its directory is `/docs/CHANGELOG`, and a new file needs to be created for each minor version. For example, all changelogs for version 1.2.x are placed in the CHANGELOG-1.2.md file. You can refer to the specific format in CHANGELOG-0.0.0.md.

2. Modify Version

Modify the latest version of the chart by modifying the `charts/hami/Chart.yaml` file. Please update both version and appVersion. Currently, there is a CI workflow that checks whether these two fields are consistent. If they are inconsistent, CI will report an error.

Modify the version in the `charts/hami/values` file to the latest version.
When the chart's version is updated, it will automatically trigger CI to release the chart version, automatically build the tag package, update the index file under the gh-page branch, and automatically generate a release and assert.

3. Create a release branch
After the above changes are merged into the `master` branch, based on the master branch, create a new release branch with the prefix release and only the `x` and `y` version numbers. For example, release-1.1. In version 1.1, all version releases including the z releases will be based on this branch.

4. Generate a New Tag

Based on relase branch, a new tag can be created. Its name must start with `v`. When a new tag is created, it will trigger the building of a new image and upload it to the `ghcr` image repository.

5. Update Release Description

The release description will be automatically generated in the second step, and we can add more release content. For example, link to the changelog file. e.g: See [the CHANGELOG](./CHANGELOG/CHANGELOG-0.0.0.md) for details.

6. Release the z version
Before releasing the z version, you need to ensure that all changes have been merged into the release branch, including the changelogs for the new z version release. All the changelogs for z relases should be put in one changelog file, then based on the latest relase branch generate a new tag.


1. 生成 token --> https://cpina.github.io/push-to-another-repository-docs/setup-using-personal-access-token.html#setup-personal-access-token


### known issues

- [] chart tgz cannot be generated