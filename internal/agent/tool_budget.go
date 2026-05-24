package agent

import "context"

// Doctor admin-brain turns may run longer diagnostic tool chains than normal
// chat turns, but must still respect finite loop and quota budgets.
const (
	DoctorAdminBrainMaxToolLoops            = 24
	DoctorAdminBrainMaxToolCalls            = 48
	DoctorAdminBrainMaxExecCalls            = 12
	DoctorAdminBrainMaxWebCalls             = 24
	DoctorAdminBrainMaxSubagentCalls        = 8
	DoctorAdminBrainMaxSessionToolCalls     = 512
	DoctorAdminBrainMaxSessionExecCalls     = 96
	DoctorAdminBrainMaxSessionWebCalls      = 192
	DoctorAdminBrainMaxSessionSubagentCalls = 48
)

type toolBudgetOverridesContextKey struct{}

// ToolBudgetOverrides raises runtime tool-loop and quota limits for a single turn.
// Zero values mean "use the runtime/configured default".
type ToolBudgetOverrides struct {
	MaxToolLoops            int
	MaxToolCalls            int
	MaxExecCalls            int
	MaxWebCalls             int
	MaxSubagentCalls        int
	MaxSessionToolCalls     int
	MaxSessionExecCalls     int
	MaxSessionWebCalls      int
	MaxSessionSubagentCalls int
}

func DoctorAdminBrainToolBudget() ToolBudgetOverrides {
	return ToolBudgetOverrides{
		MaxToolLoops:            DoctorAdminBrainMaxToolLoops,
		MaxToolCalls:            DoctorAdminBrainMaxToolCalls,
		MaxExecCalls:            DoctorAdminBrainMaxExecCalls,
		MaxWebCalls:             DoctorAdminBrainMaxWebCalls,
		MaxSubagentCalls:        DoctorAdminBrainMaxSubagentCalls,
		MaxSessionToolCalls:     DoctorAdminBrainMaxSessionToolCalls,
		MaxSessionExecCalls:     DoctorAdminBrainMaxSessionExecCalls,
		MaxSessionWebCalls:      DoctorAdminBrainMaxSessionWebCalls,
		MaxSessionSubagentCalls: DoctorAdminBrainMaxSessionSubagentCalls,
	}
}

func ContextWithToolBudgetOverrides(ctx context.Context, overrides ToolBudgetOverrides) context.Context {
	return context.WithValue(ctx, toolBudgetOverridesContextKey{}, overrides)
}

func ToolBudgetOverridesFromContext(ctx context.Context) ToolBudgetOverrides {
	if ctx == nil {
		return ToolBudgetOverrides{}
	}
	value, _ := ctx.Value(toolBudgetOverridesContextKey{}).(ToolBudgetOverrides)
	return value
}

func (o ToolBudgetOverrides) EffectiveMaxToolLoops(configured int) int {
	if o.MaxToolLoops > 0 {
		return o.MaxToolLoops
	}
	return configured
}

func (o ToolBudgetOverrides) effectiveLimit(configured, override int) int {
	if override > 0 {
		return override
	}
	return configured
}
