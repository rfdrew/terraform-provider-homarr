package provider

import (
	"errors"
	"maps"
	"net/http"
	"sort"

	"github.com/hashicorp/terraform-plugin-framework-validators/int64validator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// userPreferenceValues are the preferences that homarr_user and
// homarr_user_preferences both carry. Both models embed it, so reading,
// diffing and patching them is written once.
type userPreferenceValues struct {
	HomeBoardID               types.String `tfsdk:"home_board_id"`
	MobileHomeBoardID         types.String `tfsdk:"mobile_home_board_id"`
	DefaultSearchEngineID     types.String `tfsdk:"default_search_engine_id"`
	ColorScheme               types.String `tfsdk:"color_scheme"`
	ByteUnitSystem            types.String `tfsdk:"byte_unit_system"`
	FirstDayOfWeek            types.Int64  `tfsdk:"first_day_of_week"`
	PingIconsEnabled          types.Bool   `tfsdk:"ping_icons_enabled"`
	EnableRightClickOnWidgets types.Bool   `tfsdk:"enable_right_click_on_widgets"`
	OpenSearchInNewTab        types.Bool   `tfsdk:"open_search_in_new_tab"`
	DdgBangs                  types.Bool   `tfsdk:"ddg_bangs"`
	Header                    types.Object `tfsdk:"header"`
}

// fromAPI copies the preferences Homarr reports into the model.
//
// A cleared reference reads back as null, but homarr_user_preferences asks for
// that with "", so a "" already in the model is kept rather than replaced by
// null. homarr_user rejects "" at plan time, so there this is a plain copy.
//
// colorScheme and byteUnitSystem are absent from GET /api/users/{id} on
// releases that predate them, which decodes to "" rather than a real setting.
//
// The header is not among the shared fields, because only
// /api/users/preferences reports it parsed; headerFromAPI copies it.
func (v *userPreferenceValues) fromAPI(p client.UserPreferenceFields) {
	v.HomeBoardID = stringValueOrNullKeepingEmpty(p.HomeBoardID, v.HomeBoardID)
	v.MobileHomeBoardID = stringValueOrNullKeepingEmpty(p.MobileHomeBoardID, v.MobileHomeBoardID)
	v.DefaultSearchEngineID = stringValueOrNullKeepingEmpty(p.DefaultSearchEngineID, v.DefaultSearchEngineID)
	v.ColorScheme = stringValueOrNullIfEmpty(p.ColorScheme)
	v.ByteUnitSystem = stringValueOrNullIfEmpty(p.ByteUnitSystem)
	v.FirstDayOfWeek = types.Int64Value(p.FirstDayOfWeek)
	v.PingIconsEnabled = types.BoolValue(p.PingIconsEnabled)
	v.EnableRightClickOnWidgets = types.BoolValue(p.EnableRightClickOnWidgets)
	v.OpenSearchInNewTab = types.BoolValue(p.OpenSearchInNewTab)
	v.DdgBangs = types.BoolValue(p.DdgBangs)
}

// headerFromAPI copies the header layout from a /api/users/preferences
// response into the model, keeping the spelling of empty zones that the model
// already holds.
func (v *userPreferenceValues) headerFromAPI(h *client.HeaderPreferences) {
	v.Header = headerValueFromAPI(h, v.Header)
}

// preferencesPatch collects every preference the plan sets that differs from
// the prior state. Pass a zero-valued prior to mean "everything the plan sets",
// which is what creation needs.
//
// Nullable references are written as an explicit null when the plan clears
// them, which is why the patch is a map: an omitted key and a null key mean
// different things to Homarr. A plan clears one either by being null, which
// homarr_user can express, or by being "", which is how the Optional+Computed
// attributes of homarr_user_preferences say it — a null there only means
// "not configured".
func preferencesPatch(plan, prior userPreferenceValues) client.UserPreferencesPatch {
	patch := client.UserPreferencesPatch{}

	putString := func(key string, planned, previous types.String, nullable bool) {
		if planned.Equal(previous) || planned.IsUnknown() {
			return
		}
		switch {
		case nullable && (planned.IsNull() || planned.ValueString() == ""):
			patch[key] = nil
		case !planned.IsNull():
			patch[key] = planned.ValueString()
		}
	}
	putBool := func(key string, planned, previous types.Bool) {
		if planned.Equal(previous) || planned.IsUnknown() || planned.IsNull() {
			return
		}
		patch[key] = planned.ValueBool()
	}

	putString("homeBoardId", plan.HomeBoardID, prior.HomeBoardID, true)
	putString("mobileHomeBoardId", plan.MobileHomeBoardID, prior.MobileHomeBoardID, true)
	putString("defaultSearchEngineId", plan.DefaultSearchEngineID, prior.DefaultSearchEngineID, true)
	putString("colorScheme", plan.ColorScheme, prior.ColorScheme, false)
	putString("byteUnitSystem", plan.ByteUnitSystem, prior.ByteUnitSystem, false)
	putBool("pingIconsEnabled", plan.PingIconsEnabled, prior.PingIconsEnabled)
	putBool("enableRightClickOnWidgets", plan.EnableRightClickOnWidgets, prior.EnableRightClickOnWidgets)
	putBool("openSearchInNewTab", plan.OpenSearchInNewTab, prior.OpenSearchInNewTab)
	putBool("ddgBangs", plan.DdgBangs, prior.DdgBangs)

	if !plan.FirstDayOfWeek.Equal(prior.FirstDayOfWeek) && !plan.FirstDayOfWeek.IsUnknown() && !plan.FirstDayOfWeek.IsNull() {
		patch["firstDayOfWeek"] = plan.FirstDayOfWeek.ValueInt64()
	}

	// The header goes out whole, all three zones included, so that a zone
	// emptied in the configuration is emptied in Homarr too rather than kept by
	// its zone-by-zone merge.
	if !plan.Header.Equal(prior.Header) {
		if header, ok := headerPreferencesFromPlan(plan.Header); ok {
			patch["headerPreferences"] = header
		}
	}
	return patch
}

// userPreferenceAttributes returns the preference attributes that mean the same
// thing in homarr_user and homarr_user_preferences: optional, and computed so
// that leaving one out keeps whatever Homarr has. The three references are left
// to each resource, because the two spell "clear it" differently.
//
// The header layout is among them too, although homarr_user reads it from a
// different endpoint than its other preferences: see headerAttribute.
func userPreferenceAttributes() map[string]schema.Attribute {
	return map[string]schema.Attribute{
		"color_scheme": schema.StringAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Interface colour scheme. One of `auto`, `light` or `dark`.",
			Validators: []validator.String{
				stringvalidator.OneOf("auto", "light", "dark"),
			},
		},
		"byte_unit_system": schema.StringAttribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "How byte sizes are displayed. One of `binary` (KiB, MiB) or " +
				"`decimal` (kB, MB).",
			Validators: []validator.String{
				stringvalidator.OneOf("binary", "decimal"),
			},
		},
		"first_day_of_week": schema.Int64Attribute{
			Optional: true,
			Computed: true,
			MarkdownDescription: "First day of the week in calendar widgets, `0` for Sunday through " +
				"`6` for Saturday.",
			Validators: []validator.Int64{
				int64validator.Between(0, 6),
			},
		},
		"ping_icons_enabled": schema.BoolAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Show online-status ping indicators as icons rather than dots.",
		},
		"enable_right_click_on_widgets": schema.BoolAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Let widgets handle right-clicks instead of the browser context menu.",
		},
		"open_search_in_new_tab": schema.BoolAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Open search results in a new browser tab.",
		},
		"ddg_bangs": schema.BoolAttribute{
			Optional:            true,
			Computed:            true,
			MarkdownDescription: "Enable DuckDuckGo-style `!bang` shortcuts in the search bar.",
		},
		"header": headerAttribute(),
	}
}

// withUserPreferenceAttributes adds userPreferenceAttributes to a resource's own
// attributes and returns the combined map.
func withUserPreferenceAttributes(own map[string]schema.Attribute) map[string]schema.Attribute {
	maps.Copy(own, userPreferenceAttributes())
	return own
}

// homeBoardAccessNote explains which user Homarr checks a home board against,
// for the home_board_id and mobile_home_board_id descriptions.
const homeBoardAccessNote = "Homarr only checks that the API key's user can view the board, and an admin key " +
	"can view every board; the user themselves also needs view access, or their home page answers " +
	"\"Board not found\"."

// isBoardNotFound reports whether err is Homarr refusing a home board in a
// preferences patch, because the board does not exist or the API key's user
// cannot view it. It is a 404 like a missing user — or, before 2.3.0, a missing
// endpoint — so only the message tells them apart.
func isBoardNotFound(err error) bool {
	var apiErr *client.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusNotFound && apiErr.Message == "Board not found"
}

// addBoardNotFoundError reports a refused home board, against the attribute
// that named it when the patch set only one. Homarr does not say which slot it
// rejected, so a patch that set both gets a resource-level error.
func addBoardNotFoundError(diags *diag.Diagnostics, patch client.UserPreferencesPatch, err error) {
	var attributes []string
	for key, attribute := range map[string]string{"homeBoardId": "home_board_id", "mobileHomeBoardId": "mobile_home_board_id"} {
		if v, ok := patch[key]; ok && v != nil {
			attributes = append(attributes, attribute)
		}
	}
	sort.Strings(attributes)

	detail := "Homarr has no such board, or the API key's user cannot view it — Homarr checks the caller, " +
		"not the user whose preferences these are. " + err.Error()
	if len(attributes) == 1 {
		diags.AddAttributeError(path.Root(attributes[0]), "Home board not found", detail)
		return
	}
	diags.AddError("Home board not found", detail)
}
