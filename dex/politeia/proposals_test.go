// Copyright (c) 2019-2021, The Decred developers
// See LICENSE for details.
package pi

import (
	"reflect"
	"testing"
)

func TestPaginateTokens(t *testing.T) {
	tests := []struct {
		name     string
		tokens   []string
		pageSize uint32
		want     [][]string
	}{
		{
			name:     "2 tokens / 5 page size",
			tokens:   []string{"x", "y"},
			pageSize: 5,
			want: [][]string{
				{"x", "y"},
			},
		},
		{
			name:     "6 tokens / 5 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "w"},
			pageSize: 5,
			want: [][]string{
				{"x", "y", "z", "v", "q"},
				{"w"},
			},
		},
		{
			name: "21 tokens / 5 page size",
			tokens: []string{
				"x", "y", "z", "v", "q", "w", "x", "y", "z", "v", "q", "w",
				"x", "y", "z", "v", "q", "w", "q", "w", "e",
			},
			pageSize: 5,
			want: [][]string{
				{"x", "y", "z", "v", "q"},
				{"w", "x", "y", "z", "v"},
				{"q", "w", "x", "y", "z"},
				{"v", "q", "w", "q", "w"},
				{"e"},
			},
		},
		{
			name:     "2 tokens / 4 page size",
			tokens:   []string{"x", "y"},
			pageSize: 4,
			want: [][]string{
				{"x", "y"},
			},
		},
		{
			name:     "5 tokens / 4 page size",
			tokens:   []string{"x", "y", "z", "v", "q"},
			pageSize: 4,
			want: [][]string{
				{"x", "y", "z", "v"},
				{"q"},
			},
		},
		{
			name:     "9 tokens / 4 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "x", "y", "z", "v"},
			pageSize: 4,
			want: [][]string{
				{"x", "y", "z", "v"},
				{"q", "x", "y", "z"},
				{"v"},
			},
		},
		{
			name:     "2 tokens / 3 page size",
			tokens:   []string{"x", "y"},
			pageSize: 3,
			want: [][]string{
				{"x", "y"},
			},
		},
		{
			name:     "7 tokens / 3 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "w", "e"},
			pageSize: 3,
			want: [][]string{
				{"x", "y", "z"},
				{"v", "q", "w"},
				{"e"},
			},
		},
		{
			name:     "0 tokens / 2 page size",
			tokens:   []string{},
			pageSize: 2,
			want:     [][]string{},
		},
		{
			name:     "1 tokens / 2 page size",
			tokens:   []string{"x"},
			pageSize: 2,
			want: [][]string{
				{"x"},
			},
		},
		{
			name:     "10 tokens / 2 page size",
			tokens:   []string{"x", "y", "z", "v", "q", "w", "x", "y", "z", "v"},
			pageSize: 2,
			want: [][]string{
				{"x", "y"}, {"z", "v"}, {"q", "w"}, {"x", "y"}, {"z", "v"},
			},
		},
		{
			name:     "0 tokens / 1 page size",
			tokens:   []string{},
			pageSize: 1,
			want:     [][]string{},
		},
		{
			name:     "1 tokens / 1 page size",
			tokens:   []string{"x"},
			pageSize: 1,
			want: [][]string{
				{"x"},
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := paginateTokens(tt.tokens, tt.pageSize); !reflect.DeepEqual(got, tt.want) {
				t.Errorf("paginateTokens() = %v, want %v", got, tt.want)
			}
		})
	}
}
