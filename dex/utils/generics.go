// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utils

import (
	"maps"

	"golang.org/x/exp/constraints"
)

func ReverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	r := make(map[K]V, len(m))
	maps.Copy(r, m)
	return r
}

func MapItems[K comparable, V any](m map[K]V) []V {
	vs := make([]V, 0, len(m))
	for _, v := range m {
		vs = append(vs, v)
	}
	return vs
}

func MapKeys[K comparable, V any](m map[K]V) []K {
	ks := make([]K, 0, len(m))
	for k := range m {
		ks = append(ks, k)
	}
	return ks
}

func Map[F any, T any](s []F, f func(F) T) []T {
	r := make([]T, len(s))
	for i, v := range s {
		r[i] = f(v)
	}
	return r
}

func SafeSub[I constraints.Unsigned](a I, b I) I {
	if a < b {
		return 0
	}
	return a - b
}

func Clamp[I constraints.Ordered](v I, minVal I, maxVal I) I {
	return max(min(v, maxVal), minVal)
}
