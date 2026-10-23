# Security policy

Please report vulnerabilities privately to the repository maintainers. Do not include real home-directory paths or personal logs in public issues. Security fixes must include a regression test using temporary HOME fixtures.

The project has no telemetry and should never request broad sudo access. Review any change that adds a filesystem operation, process signal, network request, or dependency.
