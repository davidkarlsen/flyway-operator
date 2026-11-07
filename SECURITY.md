# Security policy

## Reporting a vulnerability

Please report vulnerabilities privately through GitHub:
[Report a vulnerability](https://github.com/davidkarlsen/flyway-operator/security/advisories/new)
(the repository's **Security** tab → **Report a vulnerability**).

Do not open a public issue for a security problem. Include the affected version, how to reproduce it, and the
impact as you understand it. You should get a first response within a week.

## Supported versions

Only the latest release receives security fixes. Upgrade to it before reporting, if you can.

## Scope

This covers the operator, its container image, the OLM bundle and catalog, and the Helm chart in this repository.
Vulnerabilities in Flyway itself, or in the Flyway image the operator runs by default, should go to
[Redgate](https://github.com/flyway/flyway/security).
