#!/usr/bin/env bash
# Open or update the weekly pin-currency bump PR.
#
# Runs in .github/workflows/pin-currency.yml's bump job, after
# `go run ./cmd/pincheck bump` has rewritten the stale pins in the working tree.
# The job's GITHUB_TOKEN does the pushing and the PR calls (locked decision
# bump_ci): no long-lived write credential exists for this.
#
#   - Clean tree: every stale pin was skipped or refused, so there is nothing
#     to push and no PR to open. pushed=false.
#   - The remote bump branch carries a commit the bot did not write: someone
#     is working on it. Never force-push over that — comment on the open PR
#     instead. pushed=false.
#   - Otherwise: commit as the bot on a branch rebuilt from the checked-out
#     base, force-push it, and open the PR — or update the one already open, so
#     a second weekly run never opens a second PR. pushed=true.
#
# Env: BUMP_BRANCH (default pin-currency/bump), BASE_BRANCH (default main),
# GH_TOKEN for gh, GITHUB_OUTPUT (optional) to receive pushed=true|false —
# the workflow dispatches CI on the branch only when something was pushed.
set -euo pipefail

branch="${BUMP_BRANCH:-pin-currency/bump}"
base="${BASE_BRANCH:-main}"
bot_name="github-actions[bot]"
bot_email="41898282+github-actions[bot]@users.noreply.github.com"
title="chore(deps): bump pins Dependabot cannot reach"

output() {
	if [ -n "${GITHUB_OUTPUT:-}" ]; then
		printf '%s\n' "$1" >>"$GITHUB_OUTPUT"
	fi
}

# The number of the open PR from the bump branch, or nothing.
open_pr() {
	gh pr list --head "$branch" --base "$base" --state open --json number --jq '.[0].number // empty'
}

if [ -z "$(git status --porcelain --untracked-files=no)" ]; then
	echo "pin-bump-pr: no pin changed — nothing to push"
	output "pushed=false"
	exit 0
fi

# Does the remote bump branch exist? ls-remote --exit-code answers 2 for "no
# such ref"; anything else non-zero is a failure to ask, and a failure to ask
# must not be read as "safe to force-push".
set +e
git ls-remote --exit-code --heads origin "refs/heads/$branch" >/dev/null
rc=$?
set -e
case "$rc" in
0)
	git fetch --quiet origin "+refs/heads/$branch:refs/remotes/origin/$branch" "+refs/heads/$base:refs/remotes/origin/$base"
	foreign=$(git log --format='%ae' "refs/remotes/origin/$base..refs/remotes/origin/$branch" | grep -vxF "$bot_email" || true)
	if [ -n "$foreign" ]; then
		note="The weekly pin-currency run found newer pins, but \`$branch\` carries commits the bot did not write, so it was not force-pushed. Merge or close this PR and the next run will rebuild the branch."
		pr=$(open_pr)
		if [ -n "$pr" ]; then
			gh pr comment "$pr" --body "$note"
			echo "pin-bump-pr: $branch has non-bot commits — commented on PR #$pr, branch left alone"
		else
			echo "::warning::pin-bump-pr: $branch has non-bot commits and no open PR — branch left alone"
		fi
		output "pushed=false"
		exit 0
	fi
	;;
2) ;;
*)
	echo "pin-bump-pr: could not list origin's branches (git ls-remote exit $rc)" >&2
	exit 1
	;;
esac

changed=$(git diff --name-only)
body="Pins rewritten by \`go run ./cmd/pincheck bump\` — each to the newest release on its line that has been public for more than 7 days:

$(printf '%s\n' "$changed" | sed 's/^/- /')

Opened by the weekly pin-currency workflow. CI is dispatched on this branch; merge when it is green."

git checkout --quiet -B "$branch"
git add --update
git -c "user.name=$bot_name" -c "user.email=$bot_email" commit --quiet -m "$title" -m "$body"
git push --quiet --force origin "HEAD:refs/heads/$branch"

pr=$(open_pr)
if [ -n "$pr" ]; then
	gh pr edit "$pr" --body "$body"
	echo "pin-bump-pr: force-pushed $branch and updated PR #$pr"
else
	gh pr create --base "$base" --head "$branch" --title "$title" --body "$body"
	echo "pin-bump-pr: pushed $branch and opened a PR"
fi
output "pushed=true"
