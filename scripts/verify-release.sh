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
# The install commands and the README banner are checked for the same reason.
# They were prose rather than pins, so nothing caught them going stale, and
# v0.2.0 shipped with both still telling a reader to install v0.1.9.
fail=0
for pin in \
  "cmd/simplycubed/main.go:latestKnownWorkflowTag = \"${version}\"" \
  "docs/templates/simplycubed-caller.yml:simplycubed.yml@${version}" \
  ".github/workflows/simplycubed.yml:default: ${version}" \
  "README.md:Current release: \`${version}\`" \
  "README.md:simplycubed@${version}" \
  "docs/setup.md:simplycubed@${version}"
do
  file="${pin%%:*}"
  needle="${pin#*:}"
  if ! grep -qF "${needle}" "${file}"; then
    echo "${file} does not pin ${version} (expected to find: ${needle})" >&2
    fail=1
  fi
done
[ "${fail}" -eq 0 ] || exit 1

echo "release ${version} is consistent: changelog entry present, all pins agree"
