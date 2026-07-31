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

# Workflow files have to parse, and no mapping may repeat a key. Duplicate keys
# matter more than they look: yaml.safe_load accepts them and keeps the last one,
# so a second `env:` in a step silently discards the first. GitHub is stricter
# and refuses to start the run at all. That combination is the worst one, a green
# gate and a workflow that never executes, and it is how v0.1.7 shipped with
# GH_TOKEN and both Azure variables dropped from the step that needs them.
for f in .github/workflows/*.yml docs/templates/*.yml cmd/simplycubed/*.yml.tmpl; do
  [ -e "$f" ] || continue
  # The embedded template carries a placeholder tag that is substituted at
  # render time; it is still valid YAML.
  if ! python3 - "$f" <<'PY' 2>&1
import sys, yaml

class StrictLoader(yaml.SafeLoader):
    pass

def no_duplicates(loader, node, deep=False):
    seen = set()
    for key_node, _ in node.value:
        key = loader.construct_object(key_node, deep=deep)
        if key in seen:
            raise yaml.YAMLError(
                "duplicate key %r at line %d" % (key, key_node.start_mark.line + 1)
            )
        seen.add(key)
    return yaml.SafeLoader.construct_mapping(loader, node, deep)

StrictLoader.add_constructor(
    yaml.resolver.BaseResolver.DEFAULT_MAPPING_TAG, no_duplicates
)

try:
    yaml.load(open(sys.argv[1]), Loader=StrictLoader)
except Exception as exc:
    sys.exit(str(exc))
PY
  then
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
