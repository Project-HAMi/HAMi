# Environment Dependencies Policy

## Purpose

This policy establishes guidelines for managing third-party packages in the HAMi repository. Its goal is to ensure that all dependencies are secure, up-to-date, and necessary for the project’s functionality.

## Scope

This policy applies to all maintainers of the HAMi repository and governs all third-party packages incorporated into the project.

## Policy

Maintainers must adhere to the following when incorporating third-party packages:

- **Necessity:** Include only those packages that are essential to the project’s functionality.
- **Latest Stable Versions:** Use the latest stable releases whenever possible.
- **Security:** Avoid packages with known security vulnerabilities.
- **Version Pinning:** Lock all dependencies to specific versions to maintain consistency.
- **Dependency Management:** Utilize an appropriate dependency management tool (e.g., Go modules, npm, pip) to handle third-party packages.
- **Testing:** Ensure that any new dependency passes all automated tests before integration.

## Procedure

When adding a new third-party package, maintainers should:

1. **Assess Need:** Determine whether the package is truly necessary for the project.
2. **Conduct Research:** Review the package’s maintenance status and reputation within the community.
3. **Select Version:** Opt for the latest stable version that meets the project’s requirements.
4. **Pin the Version:** Explicitly pin the dependency to the chosen version within the repository.
5. **Update Documentation:** Revise the project documentation to include details about the new dependency.

## Archive/Deprecation

If a third-party package becomes deprecated or discontinued, maintainers must promptly identify and integrate a suitable alternative while updating the documentation accordingly.

## Enforcement

Compliance with this policy is monitored by the HAMi maintainers. All dependency-related changes are subject to peer review to ensure adherence to these guidelines.

## Exceptions

Exceptions to this policy may be granted by the HAMi project lead on a case-by-case basis. Any exceptions must be documented with a clear rationale.

## Credits

This policy has been adapted and optimized based on guidelines from the [Kubescape Community](https://github.com/kubescape/kubescape/blob/master/docs/environment-dependencies-policy.md).