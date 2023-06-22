SITE_DIR=$(cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P)
BB_FILEPATH=${SITE_DIR}/src/html/bodybuilder.tmpl
CSS_DIR=${SITE_DIR}/src/css
CSS_FILE=${SITE_DIR}/dist/style.css
JS_DIR=${SITE_DIR}/src/js
JS_FILE=${SITE_DIR}/dist/entry.js

if [[ "$OSTYPE" == "darwin"* ]]; then
   hashfunc="shasum"
else
   hashfunc="sha1sum"
fi

hashfile () { "${hashfunc}" $1 | cut -d " " -f1 ; }

hashdir () {
    HASHBUF=""
    while read FP ; do
        HASHBUF="${HASHBUF}$(hashfile ${FP})"
    done < <(git ls-files "$1")
    echo ${HASHBUF} | "${hashfunc}" | cut -d " " -f1 | cut -c1-8
}

# hashcsssrc hashes the css source directory.
hashcsssrc () { hashdir "${CSS_DIR}" ; }

# hashcssdist hashes the compiled css.
hashcssdist () { hashfile "${CSS_FILE}" | cut -c1-8 ; }

# csssrcbuster parses the source directory portion of the CSS cache buster from
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

# cssdistbusterparses the output file portion of the CSS cache buster from
# bodybuilder.tmpl file. Compare with output from hashcssdist to ensure the
# cache buster is updated.
cssdistbuster () {
    HASHES=$(sed -rn 's/.*style\.css\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f2
}

# setcssbuster sets the cache buster for CSS.
setcssbuster () {
    sed -i.tmp "s/href=\"\/css\/style.css?v=\([^\"]*\)\"/href=\"\/css\/style.css?v=$(hashcsssrc)|$(hashcssdist)\"/" "${BB_FILEPATH}"
    rm "${BB_FILEPATH}.tmp"
}

# hashjssrc hashes the js source directory.
hashjssrc () { hashdir "${JS_DIR}" ; }

# hashjssrc hashes the compiled js.
hashjsdist () {
    cp "${JS_FILE}" js.tmp
    if [[ "$OSTYPE" == "darwin"* ]]; then
        sed -i '' 's/commitHash="[^"]*"//' js.tmp
    else
        sed -i 's/commitHash="[^"]*"//' js.tmp
    fi
    HASH=$(hashfile js.tmp | cut -c1-8)
    rm js.tmp
    echo ${HASH}
}

# jssrcbuster parses the source directory portion of the JS cache buster from
# bodybuilder.tmpl file. Compare the output of jssrcbuster with the hashjssrc to
# ensure that the cache buster was updated. See csssrcbuster docs regarding
# discarding the output file hash.
jssrcbuster () {
    HASHES=$(sed -rn 's/.*entry\.js\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f1
}

# jsdistbuster parses the output file portion of the JS cache buster from
# bodybuilder.tmpl. Compare with output from hashjssdist to ensure the cache
# buster is updated.
jsdistbuster () {
    HASHES=$(sed -rn 's/.*entry\.js\?v=([^"]*).*/\1/p' "${BB_FILEPATH}")
    echo ${HASHES} | cut -d'|' -f2
}

# setjsbuster sets the cache buster for JS.
setjsbuster () {
    sed -i.tmp "s/src=\"\/js\/entry.js?v=\([^\"]*\)\"/src=\"\/js\/entry.js?v=$(hashjssrc)|$(hashjsdist)\"/" "${BB_FILEPATH}"
    rm "${BB_FILEPATH}.tmp"
}
