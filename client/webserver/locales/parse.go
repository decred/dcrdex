package locales

import "regexp"

var keyRegExp = regexp.MustCompile(`\[\[\[([a-zA-Z0-9 _.-]+)\]\]\]`)

// Tokens returns a slice of tuples (token, key), where token is just key with
// triple square brackets, and could also be constructed simply as
// "[[[" + key + "]]]"". The key is the lookup key for the Locales map. The
// token is what must be replaced in the raw template.
func Tokens(tmpl []byte) [][][]byte {
	return keyRegExp.FindAllSubmatch(tmpl, -1)
}
