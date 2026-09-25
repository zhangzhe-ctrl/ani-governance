package main

import (
	"t09desc/common"

	upstream "t09desc/upstream/redact/v1"
)

func main() {
	common.Emit(upstream.File_redact_v1_redact_proto)
}
