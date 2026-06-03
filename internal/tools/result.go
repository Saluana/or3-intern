package tools

import "or3-intern/internal/actionresult"

type (
	ToolResult            = actionresult.Result
	ApprovalRequiredError = actionresult.ApprovalRequiredError
)

var (
	EncodeToolResult  = actionresult.Encode
	DecodeToolResult  = actionresult.Decode
	EncodeToolFailure = actionresult.EncodeFailure
)
