#!/usr/bin/env bash
SITE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P)
cd "${SITE_DIR}"; npm run build; cd -
source ${SITE_DIR}/cache_utilities.bash
setcssbuster
setjsbuster

