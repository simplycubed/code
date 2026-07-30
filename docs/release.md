# Release Process

`v0.1.0` is the first tagged SimplyCubed Code release. A tag means the code at
that commit passed the repo gate (`make check`), the CLI version reports that
same release, and the maintainer published release notes for it.

## Versioning

Tags use semver with a leading `v` (`v0.1.0`, `v0.2.0`, ...). Before `v1.0.0`,
minor releases may still make breaking changes when the product or CLI shape
needs it. Patch releases are for backward-compatible fixes on an existing line.

## What to publish

For each tag:

1. Update `CHANGELOG.md`.
2. Confirm `go install github.com/simplycubed/code/cmd/simplycubed@<tag>` and
   `simplycubed version` report the same version without the leading `v`.
3. Update `latestKnownWorkflowTag` in `cmd/simplycubed/main.go`.
4. Push the tag and publish matching GitHub release notes.

## Binary releases

For `v0.1.0`, binary archives are deferred. The supported install path is
`go install ...@<tag>`. If binary releases are added later, use GitHub Releases
and stamp the same version via `-ldflags`.
