#!/bin/bash

# Usage : stats.sh <git-sha-or-tag> <git-sha-or-tag>
git diff $1 $2 --shortstat
git log --numstat --format='%aN' $1..$2 | awk '
NF == 1 { author = $1 }
NF == 2 { author = $1 $2 }
NF == 3 { added[author] += $1; removed[author] += $2 }
END {
  for (author in added)
    printf "- %s: +%d, -%d, total: %d\n", author, added[author], removed[author], added[author] - removed[author]
}'

echo '

'

git log $1..$2 --format="- %s" | gawk '{print gensub(/\(#([^)]*)\)/, "([#\\1](https://github.com/decred/dcrdex/pull/\\1))", "g")}'
