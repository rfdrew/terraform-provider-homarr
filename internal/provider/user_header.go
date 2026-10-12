package provider

import (
	"errors"
	"net/http"

	"github.com/hashicorp/terraform-plugin-framework-validators/listvalidator"
	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/objectplanmodifier"
	"github.com/hashicorp/terraform-plugin-framework/resource/schema/planmodifier"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// The header layout, headerPreferences in Homarr, is the one user preference
// that is an object rather than a scalar. It is managed as a whole: when the
// configuration sets `header`, every field and all three zones are sent, so the
// stored layout is exactly the configured one. Homarr's PATCH would otherwise
// keep any zone the request leaves out.

// headerBuiltinItems are headerBuiltinItemIds in Homarr's
// packages/validation/src/header-preferences.ts.
var headerBuiltinItems = []string{
	"logo", "search", "home", "boardSwitcher", "assistant", "docker",
	"boardEdit", "boardSettings", "settings", "themeToggle", "user",
}

// headerZones are the header's zones, in the order Homarr lays them out.
var headerZones = []string{"left", "center", "right"}

// headerItemAttrTypes and headerAttrTypes describe the `header` attribute, for
// building its values outside of a plan or state.
var (
	headerItemAttrTypes = map[string]attr.Type{
		"builtin":  types.StringType,
		"board_id": types.StringType,
	}
	headerItemType  = types.ObjectType{AttrTypes: headerItemAttrTypes}
	headerZoneType  = types.ListType{ElemType: headerItemType}
	headerAttrTypes = map[string]attr.Type{
		"visible":        types.BoolType,
		"search_display": types.StringType,
		"logo_display":   types.StringType,
		"left":           headerZoneType,
		"center":         headerZoneType,
		"right":          headerZoneType,
	}
)

// headerAttribute is the `header` preference shared by homarr_user and
// homarr_user_preferences. Like the scalar preferences it is optional and
// computed: leaving it out keeps whatever layout Homarr has.
//
// The object as a whole keeps its prior state while unconfigured. No other
// attribute's change can alter the layout, so this spares every plan a
// "(known after apply)" header. A null prior header is not kept, though: state
// written before the attribute existed, or while Homarr predated 2.3.0, holds
// null, and the apply then reads a layout where the plan promised none.
//
// visible, search_display and logo_display are required rather than defaulted:
// a default on an attribute nested in an optional, computed object would also
// be applied to the prior state carried while `header` is not configured, and
// overwrite what Homarr reports.
func headerAttribute() schema.SingleNestedAttribute {
	return schema.SingleNestedAttribute{
		Optional: true,
		Computed: true,
		MarkdownDescription: "Layout of the header bar shown above boards. Leave it out to keep whatever " +
			"Homarr has; once set, the whole layout is managed, and a zone left out is emptied.\n\n" +
			"Homarr requires every item to be unique across the three zones, at most 100 items per zone, " +
			"and the `user` builtin — the account menu — somewhere in the header; the provider checks the " +
			"same at plan time. A board shortcut must point to a board the user can view, or Homarr " +
			"answers \"One or more board shortcuts are unavailable to this user\". Deleting a board does " +
			"not remove its shortcut.\n\n" +
			"~> **Requires Homarr 2.3.0 or newer.** On an older release the attribute is reported as `null` " +
			"and cannot be set. Homarr 2.0 to 2.2 store the same layout, but report it only as the raw text " +
			"they store, possibly in a legacy format, so the provider deliberately leaves it alone there.",
		PlanModifiers: []planmodifier.Object{
			objectplanmodifier.UseNonNullStateForUnknown(),
		},
		Validators: []validator.Object{
			headerItemsValidator{},
		},
		Attributes: map[string]schema.Attribute{
			"visible": schema.BoolAttribute{
				Required:            true,
				MarkdownDescription: "Whether the header is shown. Homarr's default is `true`.",
			},
			"search_display": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "How the `search` item is shown: `input` for a search field, `icon` for a " +
					"button that opens one. Homarr's default is `input`.",
				Validators: []validator.String{
					stringvalidator.OneOf("input", "icon"),
				},
			},
			"logo_display": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "How the `logo` item is shown: `logo` alone, or `logoAndText` with the " +
					"page title beside it. Homarr's default is `logoAndText`.",
				Validators: []validator.String{
					stringvalidator.OneOf("logo", "logoAndText"),
				},
			},
			"left":   headerZoneAttribute("left"),
			"center": headerZoneAttribute("center"),
			"right":  headerZoneAttribute("right"),
		},
	}
}

func headerZoneAttribute(zone string) schema.ListNestedAttribute {
	return schema.ListNestedAttribute{
		Optional: true,
		MarkdownDescription: "Items in the " + zone + " zone, in display order, at most 100. Leave it out " +
			"for an empty zone.",
		Validators: []validator.List{
			listvalidator.SizeAtMost(100),
		},
		NestedObject: schema.NestedAttributeObject{
			Attributes: map[string]schema.Attribute{
				"builtin": schema.StringAttribute{
					Optional: true,
					MarkdownDescription: "A builtin control: `logo`, `search`, `home`, `boardSwitcher`, " +
						"`assistant`, `docker`, `boardEdit`, `boardSettings`, `settings`, `themeToggle` or " +
						"`user`. Exactly one of `builtin` and `board_id` must be set.",
					Validators: []validator.String{
						stringvalidator.OneOf(headerBuiltinItems...),
						stringvalidator.ExactlyOneOf(path.MatchRelative().AtParent().AtName("board_id")),
					},
				},
				"board_id": schema.StringAttribute{
					Optional:            true,
					MarkdownDescription: "Identifier of a board to link to.",
					Validators: []validator.String{
						jsLengthBetweenValidator(1, 255),
					},
				},
			},
		},
	}
}

// headerValueFromAPI converts the header Homarr reports into the `header`
// attribute. A nil header, which a release before 2.3.0 cannot report, is null.
//
// Homarr always reports all three zones, but a zone the configuration leaves
// out is null in the plan. An empty zone therefore stays null when current —
// the plan, or the prior state — has it null or holds no layout at all, and
// only becomes [] when current says [], so that either spelling of an empty
// zone survives the read-back.
func headerValueFromAPI(h *client.HeaderPreferences, current types.Object) types.Object {
	if h == nil {
		return types.ObjectNull(headerAttrTypes)
	}

	currentZones := map[string]attr.Value{}
	if !current.IsNull() && !current.IsUnknown() {
		currentZones = current.Attributes()
	}
	zone := func(name string, items []client.HeaderItem) attr.Value {
		if len(items) == 0 {
			if prior, ok := currentZones[name].(types.List); !ok || prior.IsNull() || prior.IsUnknown() {
				return types.ListNull(headerItemType)
			}
		}
		values := make([]attr.Value, 0, len(items))
		for _, item := range items {
			values = append(values, headerItemValue(item))
		}
		return types.ListValueMust(headerItemType, values)
	}

	return types.ObjectValueMust(headerAttrTypes, map[string]attr.Value{
		"visible":        types.BoolValue(h.Visible),
		"search_display": types.StringValue(h.SearchDisplay),
		"logo_display":   types.StringValue(h.LogoDisplay),
		"left":           zone("left", h.Zones.Left),
		"center":         zone("center", h.Zones.Center),
		"right":          zone("right", h.Zones.Right),
	})
}

// headerItemValue converts one header item. An item of a type this provider
// does not know, which a newer Homarr might report, has neither field set; the
// plan-time validation then makes a configuration that keeps it impossible,
// which is the honest outcome for an item it cannot write back.
func headerItemValue(item client.HeaderItem) attr.Value {
	builtin, board := types.StringNull(), types.StringNull()
	switch item.Type {
	case "builtin":
		builtin = types.StringValue(item.ID)
	case "board":
		board = types.StringValue(item.BoardID)
	}
	return types.ObjectValueMust(headerItemAttrTypes, map[string]attr.Value{
		"builtin":  builtin,
		"board_id": board,
	})
}

// headerPreferencesFromPlan converts a planned `header` into the full layout
// Homarr takes. It reports false when the value is null or not yet fully known,
// in which case nothing should be sent. A null zone is sent as an empty one.
func headerPreferencesFromPlan(v types.Object) (client.HeaderPreferences, bool) {
	if v.IsNull() || v.IsUnknown() {
		return client.HeaderPreferences{}, false
	}
	attrs := v.Attributes()

	visible, ok1 := attrs["visible"].(types.Bool)
	searchDisplay, ok2 := attrs["search_display"].(types.String)
	logoDisplay, ok3 := attrs["logo_display"].(types.String)
	if !ok1 || !ok2 || !ok3 || visible.IsUnknown() || searchDisplay.IsUnknown() || logoDisplay.IsUnknown() {
		return client.HeaderPreferences{}, false
	}

	header := client.HeaderPreferences{
		Version:       client.HeaderPreferencesVersion,
		Visible:       visible.ValueBool(),
		SearchDisplay: searchDisplay.ValueString(),
		LogoDisplay:   logoDisplay.ValueString(),
	}
	zones := map[string]*[]client.HeaderItem{
		"left":   &header.Zones.Left,
		"center": &header.Zones.Center,
		"right":  &header.Zones.Right,
	}
	for name, target := range zones {
		items, known := headerZoneItems(attrs[name])
		if !known {
			return client.HeaderPreferences{}, false
		}
		*target = items
	}
	return header, true
}

// headerZoneItems converts one zone, reporting false while any part of it is
// unknown. A null zone is empty.
func headerZoneItems(v attr.Value) ([]client.HeaderItem, bool) {
	zone, ok := v.(types.List)
	if !ok || zone.IsUnknown() {
		return nil, false
	}
	items := []client.HeaderItem{}
	for _, element := range zone.Elements() {
		item, ok := headerItemFromValue(element)
		if !ok {
			return nil, false
		}
		items = append(items, item)
	}
	return items, true
}

// headerItemFromValue converts one configured item, reporting false while it is
// unknown or names neither a builtin nor a board.
func headerItemFromValue(v attr.Value) (client.HeaderItem, bool) {
	object, ok := v.(types.Object)
	if !ok || object.IsNull() || object.IsUnknown() {
		return client.HeaderItem{}, false
	}
	builtin, _ := object.Attributes()["builtin"].(types.String)
	board, _ := object.Attributes()["board_id"].(types.String)
	switch {
	case builtin.IsUnknown() || board.IsUnknown():
		return client.HeaderItem{}, false
	case !builtin.IsNull():
		return client.HeaderItem{Type: "builtin", ID: builtin.ValueString()}, true
	case !board.IsNull():
		return client.HeaderItem{Type: "board", BoardID: board.ValueString()}, true
	}
	return client.HeaderItem{}, false
}

// isHeaderShortcutRefused reports whether err is Homarr refusing a board
// shortcut in a header patch: a 400 for a board that does not exist or that the
// user cannot view.
func isHeaderShortcutRefused(err error) bool {
	var apiErr *client.Error
	return errors.As(err, &apiErr) && apiErr.StatusCode == http.StatusBadRequest &&
		apiErr.Message == "One or more board shortcuts are unavailable to this user"
}

// addHeaderShortcutError reports a refused board shortcut against `header`.
func addHeaderShortcutError(diags *diag.Diagnostics, err error) {
	diags.AddAttributeError(
		path.Root("header"),
		"Header board shortcut unavailable",
		"A board_id in header names a board that does not exist or that the user cannot view. Unlike home "+
			"boards, which Homarr checks against the API key's user, shortcuts are checked against the user "+
			"whose header it is, and only those the header did not hold before. "+err.Error(),
	)
}
