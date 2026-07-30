#!/bin/sh
# Part of the gate. `make check` builds and tests Go, which means a broken
# workflow file passes it: YAML is never compiled by anything here.
#
# That is not hypothetical. A merge left conflict markers in
# .github/workflows/simplycubed.yml, the gate went green, and the file reached
# main with the markers in it and a setting we had deliberately removed.
set -eu

fail=0

# Conflict markers anywhere in the tree. A merge that leaves these behind can
# otherwise be committed by `git add -A` and pass every other check.
if grep -rIn --exclude-dir=.git -E '^(<{7}|={7}|>{7})( |$)' . 2>/dev/null; then
  echo "conflict markers found (above)" >&2
  fail=1
fi

# Workflow files have to parse. Without this nothing in the gate reads them.
for f in .github/workflows/*.yml docs/templates/*.yml cmd/simplycubed/*.yml.tmpl; do
  [ -e "$f" ] || continue
  # The embedded template carries a placeholder tag that is substituted at
  # render time; it is still valid YAML.
  if ! python3 -c "import sys,yaml; yaml.safe_load(open(sys.argv[1]))" "$f" 2>/dev/null; then
    echo "not valid YAML: $f" >&2
    fail=1
  fi
done

# The sandbox is not disabled anywhere in this repository. See docs/faq.md.
if grep -rn "danger-full-access\|dangerously-bypass\|dangerously-skip-permissions" \
    .github/ docs/templates/ cmd/ 2>/dev/null | grep -v "^docs/faq.md" | grep -v "SkipPermissions passes" | grep -v "_test.go"; then
  echo "an engine safety control is disabled in a shipped file (above)" >&2
  fail=1
fi

[ "$fail" -eq 0 ] || exit 1
echo "workflows: parse, no conflict markers, no disabled safety controls"
