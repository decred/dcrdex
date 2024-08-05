// This code is available on the terms of the project LICENSE.md file,
// also available online at https://blueoakcouncil.org/license/1.0.0.

package utils

import "golang.org/x/exp/constraints"

func ReverseSlice[T any](s []T) {
	for i, j := 0, len(s)-1; i < j; i, j = i+1, j-1 {
		s[i], s[j] = s[j], s[i]
	}
}

func CopyMap[K comparable, V any](m map[K]V) map[K]V {
	r := make(map[K]V, len(m))
	for k, v := range m {
		r[k] = v
	}
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

func Min[I constraints.Ordered](m I, ns ...I) I {
	min := m
	for _, n := range ns {
		if n < min {
			min = n
		}
	}
	return min
}

func Max[I constraints.Ordered](m I, ns ...I) I {
	max := m
	for _, n := range ns {
		if n > max {
			max = n
		}
	}
	return max
}

func Clamp[I constraints.Ordered](v I, min I, max I) I {
	if v < min {
		v = min
	} else if v > max {
		v = max
	}
	return v
}
