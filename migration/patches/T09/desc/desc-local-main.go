package main

import (
	"t09desc/common"

	local "t09desc/local/redact/v1"
)

func main() {
	common.Emit(local.File_redact_v1_redact_proto)
}
