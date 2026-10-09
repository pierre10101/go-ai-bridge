#!/usr/bin/env bash
# Post or update the bridge-en intent-and-English comment on a pull request.
# Read-only on the repository; writes only that one issue comment.
#   BRIDGE_EN_FEATURES  glob(s) of feature directories, e.g. "features/*/"
#   BRIDGE_EN_BASE      base commit (the pull request's base SHA)
#   BRIDGE_EN_MAX       comment size limit in characters
#   PR_NUMBER, GITHUB_REPOSITORY, GH_TOKEN  set by the action
# BRIDGE_EN_DRY_RUN=1 prints the comment instead of posting it.
set -euo pipefail
command -v bridge-en >/dev/null || { echo "::error::bridge-en is not on PATH; run pierre10101/go-ai-bridge/.github/actions/setup-bridge-en first"; exit 1; }
[ -n "${BRIDGE_EN_BASE:-}" ] || { echo "::error::no base commit: run on pull_request events or pass base"; exit 1; }
git cat-file -e "${BRIDGE_EN_BASE}^{commit}" 2>/dev/null || { echo "::error::base ${BRIDGE_EN_BASE} is not in the checkout; use actions/checkout with fetch-depth: 0"; exit 1; }

shopt -s nullglob
read -r -a globs <<<"${BRIDGE_EN_FEATURES:-features/*/}"
dirs=()
for g in "${globs[@]}"; do
  # shellcheck disable=SC2206 # the glob is meant to expand
  matches=( $g )
  dirs+=("${matches[@]}")
done
# Features the PR deletes are not on disk; name them from the diff too.
while IFS= read -r f; do
  d=$(dirname "$f")
  for g in "${globs[@]}"; do
    # shellcheck disable=SC2053
    [[ "$d/" == ${g%/}/ || "$d" == ${g%/} ]] && dirs+=("$d")
  done
done < <(git diff --name-only --relative --diff-filter=D "${BRIDGE_EN_BASE}...HEAD" -- '*/intent.md')

if [ "${#dirs[@]}" -eq 0 ]; then
  echo "no feature directories match ${BRIDGE_EN_FEATURES}"
  body=$(printf '<!-- bridge-en:pr-english -->\n## bridge-en: intent and English\n\nNo feature directories match `%s`.\n' "${BRIDGE_EN_FEATURES}")
else
  body=$(bridge-en pr-comment -base "${BRIDGE_EN_BASE}" -max "${BRIDGE_EN_MAX:-60000}" "${dirs[@]}")
fi
marker=$(head -n1 <<<"$body")

if [ -n "${GITHUB_STEP_SUMMARY:-}" ]; then
  printf '%s\n' "$body" >>"$GITHUB_STEP_SUMMARY"
fi
if [ "${BRIDGE_EN_DRY_RUN:-}" = 1 ]; then
  printf '%s\n' "$body"
  exit 0
fi
[ -n "${PR_NUMBER:-}" ] || { echo "::error::not a pull_request event: no PR number"; exit 1; }
if [ -n "${PR_HEAD_REPO:-}" ] && [ "${PR_HEAD_REPO}" != "${GITHUB_REPOSITORY}" ]; then
  # A fork's pull request gets a read-only token: the comment is in the job summary.
  echo "::notice::pull request from the fork ${PR_HEAD_REPO}: not commenting (read-only token); see the job summary"
  exit 0
fi

# Idempotent: update the one comment that carries the marker, else create it.
existing=$(gh api "repos/${GITHUB_REPOSITORY}/issues/${PR_NUMBER}/comments" --paginate \
  --jq ".[] | select(.user.type == \"Bot\" and (.body | startswith(\"${marker}\"))) | .id" | head -n1 || true)
payload=$(jq -n --arg body "$body" '{body: $body}')
if [ -n "$existing" ]; then
  id=$(gh api --method PATCH "repos/${GITHUB_REPOSITORY}/issues/comments/${existing}" --input - --jq .id <<<"$payload")
  echo "updated comment $id"
else
  id=$(gh api --method POST "repos/${GITHUB_REPOSITORY}/issues/${PR_NUMBER}/comments" --input - --jq .id <<<"$payload")
  echo "created comment $id"
fi
[ -n "${GITHUB_OUTPUT:-}" ] && echo "comment-id=$id" >>"$GITHUB_OUTPUT"
exit 0
