package provider

import (
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tftypes"
)

// keepUnchangedPlan turns a plan that changes nothing back into the prior state.
//
// For an optional and computed nested attribute — `header` — left out of the
// configuration, Terraform proposes null instead of the prior value, as it does
// for every other kind of attribute. The framework takes that for a change and
// marks every computed attribute the configuration leaves null as unknown. The
// header's own plan modifier then restores the header, but the rest stay
// "known after apply", so a configuration that changes nothing would plan an
// update on every run.
//
// Putting the prior value back into every attribute that is unknown only
// because the configuration leaves it out tells the two cases apart: if that
// reproduces the prior state exactly, nothing was going to change. An attribute
// whose configured value is unknown, or anything else that differs, keeps the
// plan as it is.
func keepUnchangedPlan(req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	if req.State.Raw.IsNull() || req.Plan.Raw.IsNull() || req.Plan.Raw.Equal(req.State.Raw) {
		return
	}

	var planned, prior, config map[string]tftypes.Value
	if req.Plan.Raw.As(&planned) != nil || req.State.Raw.As(&prior) != nil || req.Config.Raw.As(&config) != nil {
		return
	}

	candidate := make(map[string]tftypes.Value, len(planned))
	for name, value := range planned {
		if !value.IsKnown() && config[name].IsNull() {
			value = prior[name]
		}
		candidate[name] = value
	}
	if tftypes.NewValue(req.Plan.Raw.Type(), candidate).Equal(req.State.Raw) {
		resp.Plan.Raw = req.State.Raw.Copy()
	}
}
