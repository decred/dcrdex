#!/usr/bin/env bash
npm clean-install
npm run build
source cache_utilities.bash
CSS_HASH=$(hashcssdist)
CSS_BUSTER=$(cssdistbuster)
JS_HASH=$(hashjsdist)
JS_BUSTER=$(jsdistbuster)
if [ "${CSS_HASH}" != "${CSS_BUSTER}" ] || [ "${JS_HASH}" != "${JS_BUSTER}" ]; then
	printf '%s\n' "cache busters not up-to-date" >&2
	exit 1
fi
echo "cache busters are up-to-date"