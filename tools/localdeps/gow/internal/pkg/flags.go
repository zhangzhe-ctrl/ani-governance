package pkg

import (
	"strings"
)

// SplitFlagList 展开每个元素内的逗号分隔写法("-s a,b" 等价 "-s a -s b")。
// 供各子命令对 StringArray 旗标做统一展开。
func SplitFlagList(list []string) []string {
	var out []string
	for _, item := range list {
		for _, part := range strings.Split(item, ",") {
			part = strings.TrimSpace(part)
			if part != "" {
				out = append(out, part)
			}
		}
	}
	return out
}
