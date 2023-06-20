SITE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P)
BB_FILEPATH=${SITE_DIR}/src/html/bodybuilder.tmpl
CSS_DIR=${SITE_DIR}/src/css
CSS_FILE=${SITE_DIR}/dist/style.css
JS_DIR=${SITE_DIR}/src/js
JS_FILE=${SITE_DIR}/dist/entry.js

hashfile () { sha1sum $1 | cut -d " " -f1 ; }

hashdir () {
    HASHBUF=""
    while read FP ; do
        HASHBUF="${HASHBUF}$(hashfile ${FP})"
    done < <(git ls-files "$1")
    echo ${HASHBUF} | sha1sum | cut -d " " -f1 | cut -c1-8
}

# cachebuster takes a source directory and an output file and creates a
# combined hash identifier with a pipe (|) seperator.
cachebuster () {
    DIR_HASH=$(hashdir $1)
    FILE_HASH=$(hashfile $2 | cut -c1-8)
    echo "${DIR_HASH}|${FILE_HASH}"
}

# hashcsssrc hashes the css source directory.
hashcsssrc () { hashdir "${CSS_DIR}" ; }

# hashcssdist hashes the compiled css.
hashcssdist () { hashfile "${CSS_FILE}" | cut -c1-8 ; }

# csssrcbuster parses the source directory portion of the CSS cachebuster from
# bodybuilder.tmpl file. Compare the output of cssbuster with the hashcsssrc to
# ensure that the cache buster was updated. The output file hash discareded
# here, and is of limited use for comparison because the output file will vary
# depending on if the dev compiled a development or production build, and
# because we don't necessarily have those assets created yet during CI testing
# (though that could likely be remedied).
csssrcbuster () {
    HASHES=$(sed -rn 's/.*style\.css\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f1
}

# cssdistbusterparses the output file portion of the CSS cachebuster from
# bodybuilder.tmpl file. Compare with output from hashcssdist to ensure the
# cache buster is updated.
cssdistbuster () {
    HASHES=$(sed -rn 's/.*style\.css\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f2
}

# setcssbuster sets the cache buster for CSS.
setcssbuster () {
    sed -i.tmp "s/href=\"\/css\/style.css?v=\([^\"]*\)\"/href=\"\/css\/style.css?v=$(cachebuster "${CSS_DIR}" "${CSS_FILE}")\"/" "${BB_FILEPATH}"
    rm "${BB_FILEPATH}.tmp"
}

# hashjssrc hashes the js source directory.
hashjssrc () { hashdir "${JS_DIR}" ; }

# hashjssrc hashes the compiled js.
hashjsdist () { hashfile "${JS_FILE}" | cut -c1-8 ; }

# jssrcbuster parses the source directory portion of the JS cachebuster from
# bodybuilder.tmpl file. Compare the output of jssrcbuster with the hashjssrc to
# ensure that the cache buster was updated. See csssrcbuster docs regarding
# discarding the output file hash.
jssrcbuster () {
    HASHES=$(sed -rn 's/.*entry\.js\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f1
}

# jsdistbuster parses the output file portion of the JS cachebuster from
# bodybuilder.tmpl. Compare with output from hashjssdist to ensure the cache
# buster is updated.
jsdistbuster () {
    HASHES=$(sed -rn 's/.*entry\.js\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f2
}

# setjsbuster sets the cache buster for JS.
setjsbuster () {
    sed -i.tmp "s/src=\"\/js\/entry.js?v=\([^\"]*\)\"/src=\"\/js\/entry.js?v=$(cachebuster "${JS_DIR}" "${JS_FILE}")\"/" "${BB_FILEPATH}"
    rm "${BB_FILEPATH}.tmp"
}
