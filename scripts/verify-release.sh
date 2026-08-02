#!/bin/sh
# Refuse to tag a release that is not internally consistent. Every check here
# corresponds to a way a release has actually gone wrong.
set -eu

version="${1:?usage: verify-release.sh vX.Y.Z}"

case "${version}" in
  v[0-9]*.[0-9]*.[0-9]*) ;;
  *) echo "version must look like v0.1.6, got '${version}'" >&2; exit 1 ;;
esac

if git rev-parse -q --verify "refs/tags/${version}" >/dev/null 2>&1; then
  echo "tag ${version} already exists; Go module versions are immutable, so cut the next patch instead" >&2
  exit 1
fi

# v0.1.2 and v0.1.4 were both published with no changelog entry for them.
if ! grep -qx "## ${version}" CHANGELOG.md; then
  echo "CHANGELOG.md has no '## ${version}' section" >&2
  exit 1
fi

# Every pin has to name the version being released, or `init --workflow` writes
# a caller pointing at a different one than the CLI it came from.
#
# The README banner is checked for the same reason. It was prose rather than a
# pin, so nothing caught it going stale, and v0.2.0 shipped with the banner
# still announcing v0.1.9. The install commands deliberately stay as
# <release-tag> with a link to Releases, so they are not pinned here.
fail=0
for pin in \
  "cmd/simplycubed/main.go:latestKnownWorkflowTag[[:space:]]*= \"${version}\"" \
  "docs/templates/simplycubed-caller.yml:simplycubed.yml@${version}" \
  ".github/workflows/simplycubed.yml:default: ${version}" \
  "README.md:Current release: \`${version}\`"
do
  file="${pin%%:*}"
  needle="${pin#*:}"
  # Extended regex, not a fixed string: the Go pin is a constant whose alignment
  # gofmt controls, so adding another constant to the same block silently broke
  # an exact match. A release check that a formatter can invalidate is worse
  # than none, because it fails on the one change it was meant to guard.
  if ! grep -qE "${needle}" "${file}"; then
    echo "${file} does not pin ${version} (expected to find: ${needle})" >&2
    fail=1
  fi
done
[ "${fail}" -eq 0 ] || exit 1

echo "release ${version} is consistent: changelog entry present, all pins agree"
