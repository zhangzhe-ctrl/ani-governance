package utils

import (
	"strconv"
	"strings"

	"go-wind-admin/pkg/localdeps/go-utils/sliceutil"
)

func FilterBlacklist(data []string, blacklist []string) []string {
	bm := make(map[string]struct{}, len(blacklist))
	for _, s := range blacklist {
		bm[s] = struct{}{}
	}

	n := 0
	for _, x := range data {
		if _, found := bm[x]; !found {
			data[n] = x
			n++
		}
	}
	return data[:n]
}

func NumberSliceToString(numbers []uint32) string {
	return strings.Join(
		sliceutil.Map(numbers, func(value uint32, _ int, _ []uint32) string { return strconv.FormatUint(uint64(value), 10) }),
		",",
	)
}
