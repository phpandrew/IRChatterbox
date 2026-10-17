#!/usr/bin/env bash
# Turns the Security workflow's scan results into GitHub issues, one per scanner.
#   found → open an issue (or update the open one; comment only when the findings changed)
#   clean → close the open issue, if there is one
#   error → leave issues alone, but fail the run so it gets noticed
# On pull requests it only prints a summary and fails if anything was found.
set -uo pipefail
dir="${1:-results}"
repo="${GITHUB_REPOSITORY}"
failed=0

if [ "$EVENT" != "pull_request" ]; then
  gh label create security --repo "$repo" --color B60205 --description "Found by the Security workflow" --force >/dev/null 2>&1 || true
fi

# report <key> <title> <status> <findings-file> <how-to-fix>
report() {
  local key="$1" title="$2" status="$3" file="$4" fix="$5"
  echo "== $title: $status"
  if [ "$status" = "error" ]; then
    echo "::error::$title: the scan itself failed, see the job log"
    failed=1; return
  fi
  if [ "$EVENT" = "pull_request" ]; then
    if [ "$status" = "found" ]; then echo "::error::$title: findings, see the job log"; failed=1; fi
    return
  fi

  local num
  num=$(gh issue list --repo "$repo" --state open --label security --limit 100 --json number,body \
        --jq ".[] | select(.body | contains(\"<!-- security-scan:$key -->\")) | .number" | head -n1)

  if [ "$status" = "clean" ]; then
    if [ -n "$num" ]; then
      gh issue close "$num" --repo "$repo" --comment "Fixed: the latest scan is clean. ($RUN_URL)"
      echo "closed #$num"
    fi
    return
  fi

  # found: the hash lets us tell whether the findings changed since the last run
  local hash body
  hash=$(sed -E 's/[0-9]+(\.[0-9]+)?m?s\b//g' "$file" | sha256sum | cut -c1-16)
  body=$(mktemp)
  {
    echo "<!-- security-scan:$key -->"
    echo "<!-- findings:$hash -->"
    echo "The **Security** workflow found problems. This issue is updated on every scan and closes itself once they're fixed."
    echo
    echo "**How to fix:** $fix"
    echo
    echo "Latest run: $RUN_URL"
    echo
    echo '<details open><summary>Findings</summary>'
    echo
    echo '```'
    head -c 60000 "$file"
    echo '```'
    echo '</details>'
  } > "$body"

  if [ -z "$num" ]; then
    gh issue create --repo "$repo" --title "Security: $title" --label security --body-file "$body"
  else
    local old
    old=$(gh issue view "$num" --repo "$repo" --json body --jq .body | grep -o 'findings:[0-9a-f]*' | cut -d: -f2)
    gh issue edit "$num" --repo "$repo" --body-file "$body" >/dev/null
    if [ "$old" != "$hash" ]; then
      gh issue comment "$num" --repo "$repo" --body "The findings changed. Details updated above. ($RUN_URL)"
      echo "updated #$num (changed)"
    else
      echo "#$num unchanged"
    fi
  fi
}

st() { cat "$dir/$1.status" 2>/dev/null || echo error; }

report go "Go vulnerabilities (govulncheck)" "$(st govulncheck)" "$dir/govulncheck.txt" \
  "update the module named in the finding: \`cd source && go get <module>@<fixed version> && go mod tidy\`. A finding in the Go standard library means bumping the \`go\` / \`toolchain\` line in \`source/go.mod\`."

report npm "npm vulnerabilities (npm audit)" "$(st npm-audit)" "$dir/npm-audit.txt" \
  "\`cd source/frontend && npm audit fix\`, then rebuild. If it needs a major version, \`npm audit fix --force\` or update the package by hand. Dependabot may already have a PR open for it."

# CodeQL uploads to the Security tab; mirror its open alerts into an issue too
codeql_status=error
if [ "$CODEQL_RESULT" = "success" ]; then
  sleep 20 # alerts take a moment to appear after upload
  if [ "$EVENT" = "pull_request" ]; then
    codeql_status=clean # PR alerts are shown on the PR itself by CodeQL
  elif gh api "repos/$repo/code-scanning/alerts?state=open&per_page=100" \
        --jq '.[] | "[\(.rule.security_severity_level // .rule.severity)] \(.rule.description)\n    \(.most_recent_instance.location.path):\(.most_recent_instance.location.start_line)  \(.html_url)"' \
        > "$dir/codeql.txt" 2> "$dir/codeql.err"; then
    if [ -s "$dir/codeql.txt" ]; then codeql_status=found; else codeql_status=clean; fi
    cat "$dir/codeql.txt"
  else
    cat "$dir/codeql.err"
    echo "::warning::Couldn't read code scanning alerts (is code scanning enabled for the repo?)"
    codeql_status=skip
  fi
fi
[ "$codeql_status" = "skip" ] || report codeql "CodeQL alerts" "$codeql_status" "$dir/codeql.txt" \
  "open each alert link for the explanation and the exact line. Fix the code, or dismiss the alert on GitHub if it's a false positive."

exit $failed
