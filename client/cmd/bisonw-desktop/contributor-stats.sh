#!/bin/bash

# Usage : stats.sh <git-sha-or-tag>
git diff $1 HEAD --shortstat
git log --numstat --format='%aN' $1..HEAD | awk '
NF == 1 { author = $1 }
NF == 3 { added[author] += $1; removed[author] += $2 }
END {
  for (author in added)
    printf "%s: +%d, -%d, total: %d\n", author, added[author], removed[author], added[author] - removed[author]
}'
