# Release Process

`v0.1.0` is the first tagged SimplyCubed Code release. A tag means the code at
that commit passed the repo gate (`make check`), the CLI version reports that
same release, and the maintainer published release notes for it.

## Versioning

Tags use semver with a leading `v` (`v0.1.0`, `v0.2.0`, ...). Before `v1.0.0`,
minor releases may still make breaking changes when the product or CLI shape
needs it. Patch releases are for backward-compatible fixes on an existing line.

## Cutting a release

Run the **tag-release** workflow from the Actions tab and give it the version
(`v0.1.6`). It checks out the remote tip of the default branch, verifies the
release is consistent, runs the gate, and creates the tag. Pushing that tag
triggers the release workflow, which publishes the notes.

Nothing is tagged from a local checkout. Two releases were burned by a stale
clone: `git checkout main` reported being behind, the warning scrolled past, and
the tag landed on the wrong commit. A tag is public the moment it is pushed and
`proxy.golang.org` records it immutably, so each mistake cost a version number
and needed a `retract` line in `go.mod`.

`scripts/verify-release.sh` is what the workflow runs, and it refuses to tag
when the version is malformed, the tag already exists, `CHANGELOG.md` has no
section for it, or any version pin disagrees with it.

## What to publish

For each tag:

1. Update `CHANGELOG.md`.
2. Confirm `go install github.com/simplycubed/code/cmd/simplycubed@<tag>` and
   `simplycubed version` report the same version without the leading `v`.
3. Update `latestKnownWorkflowTag` in `cmd/simplycubed/main.go`, the pins in
   `docs/templates/simplycubed-caller.yml`, and the default `version` input in
   `.github/workflows/simplycubed.yml`. The release check enforces that all
   three agree with the tag.
4. Push the tag and publish matching GitHub release notes.

## Binary releases

For `v0.1.0`, binary archives are deferred. The supported install path is
`go install ...@<tag>`. If binary releases are added later, use GitHub Releases
and stamp the same version via `-ldflags`.
