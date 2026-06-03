package agent

import "or3-intern/internal/serviceerrors"

const (
	PublicErrorProvider      = serviceerrors.PublicErrorProvider
	PublicErrorStream        = serviceerrors.PublicErrorStream
	PublicErrorValidation    = serviceerrors.PublicErrorValidation
	PublicErrorPolicy        = serviceerrors.PublicErrorPolicy
	PublicErrorApproval      = serviceerrors.PublicErrorApproval
	PublicErrorToolExecution = serviceerrors.PublicErrorToolExecution
	PublicErrorLoopLimit     = serviceerrors.PublicErrorLoopLimit
	PublicErrorAbort         = serviceerrors.PublicErrorAbort
	PublicErrorUnknown       = serviceerrors.PublicErrorUnknown
)

func PublicErrorCode(err error) string {
	return serviceerrors.PublicErrorCode(err)
}
