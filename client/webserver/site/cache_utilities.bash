SITE_DIR=`cd "$(dirname "${BASH_SOURCE[0]}")" ; pwd -P`
BB_FILEPATH=${SITE_DIR}/src/html/bodybuilder.tmpl
CSS_DIR=${SITE_DIR}/src/css
JS_DIR=${SITE_DIR}/src/js

bbhash () { sha1sum ${SITE_DIR}/src/html/bodybuilder.tmpl | head -n1 | cut -d " " -f1 ; }
hashdir () { sha1sum $1/* | md5sum | head -n1 | cut -d " " -f1 ; }
setcssbuster () {
    sed -i.tmp "s/href=\"\/css\/style.css?v=\([^\"]*\)\"/href=\"\/css\/style.css?v=`hashdir ${CSS_DIR}`\"/" ${BB_FILEPATH}
    rm ${BB_FILEPATH}.tmp
}
setjsbuster () {
    sed -i.tmp "s/src=\"\/js\/entry.js?v=\([^\"]*\)\"/src=\"\/js\/entry.js?v=`hashdir ${JS_DIR}`\"/" ${BB_FILEPATH}
    rm ${BB_FILEPATH}.tmp
}
