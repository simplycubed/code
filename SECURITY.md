# Security policy

## Reporting a vulnerability

Please report security issues privately. Use GitHub's private vulnerability reporting for this repository: go to the **Security** tab and choose **Report a vulnerability**. Do not open a public issue for a suspected vulnerability.

We aim to acknowledge a report within 3 business days.

## Design constraints

SimplyCubed Code runs inside your own GitHub Actions and opens pull requests. By design it:

- never merges its own work and never pushes to protected branches,
- holds no deploy or production credentials,
- reads model-provider keys and tokens only from your own GitHub secret store, within your own Actions runs.

## Out of scope

The following are not treated as vulnerabilities in SimplyCubed Code:

- The agent proposing a low-quality or incorrect change. Whether to merge is always a human decision.
- Behavior that results from a repository's own configuration, such as granting the app more access than it needs or leaving branch protection off.

## Supported versions

This project is early and has no stable release yet. Security fixes are applied to the default branch.
