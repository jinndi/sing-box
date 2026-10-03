#!/usr/bin/env bash

set -euo pipefail

branches=(testing stable)
git_dir=$(git rev-parse --path-format=absolute --git-dir)
script_path=$(realpath "$0")

usage() {
    printf 'Usage: %s [--finish testing|stable]\n' "$script_path"
    printf 'Sync testing and stable with upstream, preserving commits authored by your configured email.\n'
}

if [[ "${1:-}" == "-h" || "${1:-}" == "--help" ]]; then
    usage
    exit 0
fi

if [[ "${1:-}" == "--finish" && $# -eq 2 ]]; then
    finish_branch="$2"
    if [[ "$finish_branch" != testing && "$finish_branch" != stable ]]; then
        printf 'Error: branch must be testing or stable.\n' >&2
        exit 2
    fi
elif [[ $# -ne 0 ]]; then
    usage >&2
    exit 2
else
    finish_branch=""
fi

original_branch=$(git branch --show-current)
if [[ -z "$original_branch" ]]; then
    printf 'Error: detached HEAD; switch to a branch first.\n' >&2
    exit 1
fi

if [[ -n "$(git status --porcelain)" ]]; then
    printf 'Error: working tree is not clean; commit or stash changes first.\n' >&2
    exit 1
fi

if [[ -d "$(git rev-parse --git-path rebase-merge)" || -d "$(git rev-parse --git-path rebase-apply)" || -f "$(git rev-parse --git-path CHERRY_PICK_HEAD)" ]]; then
    printf 'Error: a rebase or cherry-pick is already in progress. Finish or abort it first.\n' >&2
    exit 1
fi

author_email="${SYNC_AUTHOR_EMAIL:-$(git config user.email || true)}"
if [[ -z "$author_email" ]]; then
    printf 'Error: set git config user.email or SYNC_AUTHOR_EMAIL.\n' >&2
    exit 1
fi

state_file() {
    printf '%s/sync-upstream-%s.state' "$git_dir" "$1"
}

temp_branch() {
    printf 'sync-upstream/%s' "$1"
}

finish_sync() {
    local branch="$1"
    local state temporary expected_origin expected_upstream saved_branch current_origin current_upstream
    state=$(state_file "$branch")
    temporary=$(temp_branch "$branch")

    if [[ ! -f "$state" ]] || ! git show-ref --verify --quiet "refs/heads/$temporary"; then
        printf 'Error: no pending sync state for %s.\n' "$branch" >&2
        exit 1
    fi

    IFS=$'\t' read -r expected_origin expected_upstream saved_branch < "$state"
    git fetch --prune upstream
    git fetch --prune origin
    current_origin=$(git rev-parse "origin/$branch")
    current_upstream=$(git rev-parse "upstream/$branch")

    if [[ "$current_origin" != "$expected_origin" ]]; then
        printf 'Error: origin/%s changed while resolving conflicts; refusing to overwrite it.\n' "$branch" >&2
        exit 1
    fi
    if [[ "$current_upstream" != "$expected_upstream" ]]; then
        printf 'Error: upstream/%s changed while resolving conflicts; restart synchronization.\n' "$branch" >&2
        exit 1
    fi
    if [[ -f "$(git rev-parse --git-path CHERRY_PICK_HEAD)" ]]; then
        printf 'Error: finish the personal commit first with git cherry-pick --continue.\n' >&2
        exit 1
    fi
    if [[ -n "$(git status --porcelain)" ]]; then
        printf 'Error: working tree is not clean; finish conflict resolution first.\n' >&2
        exit 1
    fi

    git switch --detach "$temporary"
    git branch -f "$branch" "$temporary"
    git switch "$branch"
    git push "--force-with-lease=refs/heads/$branch:$expected_origin" origin "HEAD:refs/heads/$branch"
    git branch -D "$temporary"
    rm -f "$state"

    if [[ -n "$saved_branch" && "$saved_branch" != "$branch" ]] && git show-ref --verify --quiet "refs/heads/$saved_branch"; then
        git switch "$saved_branch"
    fi
    printf 'Synchronized %s.\n' "$branch"
}

git fetch --prune upstream
git fetch --prune origin

for branch in "${branches[@]}"; do
    if [[ -n "$finish_branch" && "$branch" != "$finish_branch" ]]; then
        continue
    fi
    if [[ -f "$(state_file "$branch")" ]]; then
        finish_sync "$branch"
        continue
    elif [[ -n "$finish_branch" ]]; then
        printf 'Error: no pending sync state for %s.\n' "$branch" >&2
        exit 1
    fi

    for ref in "refs/heads/$branch" "refs/remotes/upstream/$branch" "refs/remotes/origin/$branch"; do
        if ! git show-ref --verify --quiet "$ref"; then
            printf 'Error: required ref not found: %s\n' "$ref" >&2
            exit 1
        fi
    done

    if ! git merge-base --is-ancestor "origin/$branch" "$branch"; then
        printf 'Error: origin/%s has commits missing from local %s; reconcile them first.\n' "$branch" "$branch" >&2
        exit 1
    fi

    origin_commit=$(git rev-parse "origin/$branch")
    upstream_commit=$(git rev-parse "upstream/$branch")
    printf '\nUpdating %s from upstream/%s...\n' "$branch" "$branch"

    if git merge-base --is-ancestor "upstream/$branch" "$branch"; then
        while IFS=$'\t' read -r commit commit_email; do
            if [[ "$commit_email" != "$author_email" ]]; then
                printf 'Error: %s has a non-personal commit above upstream: %s <%s>.\n' "$branch" "$commit" "$commit_email" >&2
                exit 1
            fi
        done < <(git log --format='%H%x09%ae' --no-merges "upstream/$branch..$branch")

        merge_commit=$(git log --merges --format='%H' "upstream/$branch..$branch" | sed -n '1p')
        if [[ -n "$merge_commit" ]]; then
            printf 'Error: merge commit %s above upstream needs manual handling.\n' "$merge_commit" >&2
            exit 1
        fi

        git switch "$branch"
        git rebase "upstream/$branch"
        git push "--force-with-lease=refs/heads/$branch:$origin_commit" origin "HEAD:refs/heads/$branch"
    else
        personal_commits=()
        while IFS=$'\t' read -r commit commit_email; do
            if [[ "$commit_email" == "$author_email" ]]; then
                personal_commits+=("$commit")
            fi
        done < <(git log --format='%H%x09%ae' --reverse "refs/heads/$branch" "^refs/remotes/upstream/$branch")

        merge_commit=$(git log --merges --format='%H' "refs/remotes/upstream/$branch..refs/heads/$branch" | sed -n '1p')
        if [[ -n "$merge_commit" ]]; then
            printf 'Error: rewritten history includes merge commit %s; manual handling is required.\n' "$merge_commit" >&2
            exit 1
        fi

        temporary=$(temp_branch "$branch")
        if git show-ref --verify --quiet "refs/heads/$temporary"; then
            printf 'Error: temporary branch %s already exists; inspect it before continuing.\n' "$temporary" >&2
            exit 1
        fi

        printf '%s\t%s\t%s\n' "$origin_commit" "$upstream_commit" "$original_branch" > "$(state_file "$branch")"
        git switch -c "$temporary" "upstream/$branch"
        if [[ ${#personal_commits[@]} -gt 0 ]] && ! git cherry-pick "${personal_commits[@]}"; then
            printf '\nA personal commit conflicted. Resolve it, then run:\n' >&2
            printf '  git add <resolved-files>\n  git cherry-pick --continue\n  %s --finish %s\n' "$script_path" "$branch" >&2
            exit 1
        fi
        finish_sync "$branch"
    fi
done

if [[ -n "$finish_branch" ]]; then
    exit 0
fi

current_branch=$(git branch --show-current)
if [[ -n "$original_branch" && "$current_branch" != "$original_branch" ]] && git show-ref --verify --quiet "refs/heads/$original_branch"; then
    git switch "$original_branch"
fi

printf '\nBoth branches are synchronized.\n'
