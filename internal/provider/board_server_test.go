package provider

import (
	"context"
	"strings"
	"testing"

	"github.com/hashicorp/terraform-plugin-framework/action"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/list"
	fwprovider "github.com/hashicorp/terraform-plugin-framework/provider"
	"github.com/hashicorp/terraform-plugin-framework/providerserver"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-go/tfprotov6"
	"github.com/hashicorp/terraform-plugin-go/tftypes"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// These tests drive the board resources and the add-item action through the
// same protocol server Terraform talks to, so that what the framework does
// between the calls — schema validation, plan modifiers, private state — is
// part of what is tested. The provider is configured with the in-process fake
// Homarr instead of reaching out over HTTP.

// boardHarnessProvider is the homarr provider with a fixed client and only the
// board types registered, independent of what provider.go registers.
type boardHarnessProvider struct {
	homarrProvider
	client *client.Client
}

var _ fwprovider.ProviderWithActions = &boardHarnessProvider{}

func (p *boardHarnessProvider) Configure(_ context.Context, _ fwprovider.ConfigureRequest, resp *fwprovider.ConfigureResponse) {
	resp.ResourceData = p.client
	resp.DataSourceData = p.client
	resp.ActionData = p.client
}

func (p *boardHarnessProvider) Resources(_ context.Context) []func() resource.Resource {
	return []func() resource.Resource{NewBoardResource, NewBoardCopyResource}
}

func (p *boardHarnessProvider) DataSources(_ context.Context) []func() datasource.DataSource {
	return []func() datasource.DataSource{NewBoardDataSource, NewBoardsDataSource}
}

func (p *boardHarnessProvider) Actions(_ context.Context) []func() action.Action {
	return []func() action.Action{NewBoardAddItemAction}
}

// ListResources registers none: a list resource needs its resource, and only
// the board resources are registered here.
func (p *boardHarnessProvider) ListResources(_ context.Context) []func() list.ListResource {
	return nil
}

func boardHarnessServer(t *testing.T, fake *fakeHomarr) tfprotov6.ProviderServerWithActions {
	t.Helper()
	ctx := context.Background()
	server, err := providerserver.NewProtocol6WithError(&boardHarnessProvider{client: fake.client(t)})()
	if err != nil {
		t.Fatal(err)
	}
	withActions, ok := server.(tfprotov6.ProviderServerWithActions)
	if !ok {
		t.Fatal("the framework's protocol server does not serve actions")
	}

	var schemaResp fwprovider.SchemaResponse
	(&homarrProvider{}).Schema(ctx, fwprovider.SchemaRequest{}, &schemaResp)
	config := boardProtocolValue(t, schemaResp.Schema.Type().TerraformType(ctx), nil)
	resp, err := server.ConfigureProvider(ctx, &tfprotov6.ConfigureProviderRequest{TerraformVersion: "1.14.0", Config: &config})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, resp.Diagnostics)
	return withActions
}

// boardProtocolValue builds an object of type typ from attrs, leaving every other
// attribute null.
func boardProtocolValue(t *testing.T, typ tftypes.Type, attrs map[string]tftypes.Value) tfprotov6.DynamicValue {
	t.Helper()
	object := typ.(tftypes.Object)
	values := map[string]tftypes.Value{}
	for name, attrType := range object.AttributeTypes {
		if v, ok := attrs[name]; ok {
			values[name] = v
			continue
		}
		values[name] = tftypes.NewValue(attrType, nil)
	}
	dv, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, values))
	if err != nil {
		t.Fatal(err)
	}
	return dv
}

func nullBoardProtocolValue(t *testing.T, typ tftypes.Type) tfprotov6.DynamicValue {
	t.Helper()
	dv, err := tfprotov6.NewDynamicValue(typ, tftypes.NewValue(typ, nil))
	if err != nil {
		t.Fatal(err)
	}
	return dv
}

func requireNoProtocolErrors(t *testing.T, diags []*tfprotov6.Diagnostic) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError {
			t.Fatalf("unexpected error: %s: %s", d.Summary, d.Detail)
		}
	}
}

func requireProtocolErrorContaining(t *testing.T, diags []*tfprotov6.Diagnostic, want string) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityError && (strings.Contains(d.Summary, want) || strings.Contains(d.Detail, want)) {
			return
		}
	}
	t.Fatalf("expected an error containing %q, got %v", want, diags)
}

func tfString(s string) tftypes.Value { return tftypes.NewValue(tftypes.String, s) }
func tfNumber(n int64) tftypes.Value  { return tftypes.NewValue(tftypes.Number, n) }
func tfBool(b bool) tftypes.Value     { return tftypes.NewValue(tftypes.Bool, b) }

func requireProtocolWarningContaining(t *testing.T, diags []*tfprotov6.Diagnostic, want string) {
	t.Helper()
	for _, d := range diags {
		if d.Severity == tfprotov6.DiagnosticSeverityWarning && (strings.Contains(d.Summary, want) || strings.Contains(d.Detail, want)) {
			return
		}
	}
	t.Fatalf("expected a warning containing %q, got %v", want, diags)
}

func requiresReplaceOf(resp *tfprotov6.PlanResourceChangeResponse, attribute string) bool {
	want := tftypes.NewAttributePath().WithAttributeName(attribute)
	for _, p := range resp.RequiresReplace {
		if p.Equal(want) {
			return true
		}
	}
	return false
}

// A replacement is planned twice: against the prior state, then once more for
// the new object with a null prior state and the first plan's private state.
// The board being replaced keeps its name, and must not even be warned about in
// the second plan. Terraform currently discards that plan's warnings, so this
// pins the provider's own behaviour rather than anything a user sees.
func TestBoardReplacementPlanIgnoresItsOwnName(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"infra","isPublic":false,"isHome":false,"isMobileHome":false}]`)
	server := boardHarnessServer(t, fake)
	typ := resourceSchema(t, &boardResource{}).Type().TerraformType(ctx)

	prior := map[string]tftypes.Value{
		"id": tfString("b1"), "name": tfString("infra"), "column_count": tfNumber(10), "is_public": tfBool(false),
		"creator_id": tfString("u1"), "is_home": tfBool(false), "is_mobile_home": tfBool(false),
		"base_layout_id": tfString("l1"),
	}
	config := map[string]tftypes.Value{"name": tfString("infra"), "column_count": tfNumber(12)}
	proposed := map[string]tftypes.Value{}
	for k, v := range prior {
		proposed[k] = v
	}
	proposed["column_count"] = tfNumber(12)

	priorState := boardProtocolValue(t, typ, prior)
	proposedState := boardProtocolValue(t, typ, proposed)
	configValue := boardProtocolValue(t, typ, config)
	first, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
		TypeName:         "homarr_board",
		PriorState:       &priorState,
		ProposedNewState: &proposedState,
		Config:           &configValue,
	})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, first.Diagnostics)
	if !requiresReplaceOf(first, "column_count") {
		t.Fatalf("a column_count change did not require replacement: %v", first.RequiresReplace)
	}

	nullPrior := nullBoardProtocolValue(t, typ)
	createProposed := boardProtocolValue(t, typ, config)
	replan := func(priorPrivate []byte) *tfprotov6.PlanResourceChangeResponse {
		resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
			TypeName:         "homarr_board",
			PriorState:       &nullPrior,
			ProposedNewState: &createProposed,
			Config:           &configValue,
			PriorPrivate:     priorPrivate,
		})
		if err != nil {
			t.Fatal(err)
		}
		return resp
	}

	if diags := replan(first.PlannedPrivate).Diagnostics; len(diags) != 0 {
		t.Fatalf("the replacement was warned about its own board: %v", diags)
	}
}

// A tainted object is replaced without a plan against its prior state: the one
// request Terraform sends has a null prior state and no private state, and the
// tainted board is still listed under the planned name. The provider cannot
// tell that from a clash, so it warns, and must not fail the plan — that would
// leave the tainted board impossible to replace.
func TestBoardTaintedReplacementPlanIsNotRefused(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("GET /api/boards", 200, `[{"id":"b1","name":"infra","isPublic":false,"isHome":false,"isMobileHome":false}]`)
	server := boardHarnessServer(t, fake)

	for typeName, r := range map[string]resource.Resource{
		"homarr_board":      &boardResource{},
		"homarr_board_copy": &boardCopyResource{},
	} {
		t.Run(typeName, func(t *testing.T) {
			typ := resourceSchema(t, r).Type().TerraformType(ctx)
			config := map[string]tftypes.Value{"name": tfString("infra"), "column_count": tfNumber(10)}
			if typeName == "homarr_board_copy" {
				config = map[string]tftypes.Value{"name": tfString("infra"), "source_board_id": tfString("template")}
			}
			nullPrior := nullBoardProtocolValue(t, typ)
			configValue := boardProtocolValue(t, typ, config)
			resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
				TypeName:         typeName,
				PriorState:       &nullPrior,
				ProposedNewState: &configValue,
				Config:           &configValue,
			})
			if err != nil {
				t.Fatal(err)
			}
			requireNoProtocolErrors(t, resp.Diagnostics)
			requireProtocolWarningContaining(t, resp.Diagnostics, "Board name already in use")
		})
	}
}

// After an import source_board_id is null in state, and the first plan only
// records it; changing a recorded one replaces the copy.
func TestBoardCopySourceChangeReplacesOnlyARecordedSource(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	server := boardHarnessServer(t, fake)
	typ := resourceSchema(t, &boardCopyResource{}).Type().TerraformType(ctx)

	plan := func(priorSource tftypes.Value) *tfprotov6.PlanResourceChangeResponse {
		prior := map[string]tftypes.Value{
			"id": tfString("c1"), "name": tfString("team-a"), "is_public": tfBool(false), "creator_id": tfString("u1"),
			"is_home": tfBool(false), "is_mobile_home": tfBool(false), "source_board_id": priorSource,
		}
		proposed := map[string]tftypes.Value{}
		for k, v := range prior {
			proposed[k] = v
		}
		proposed["source_board_id"] = tfString("template")

		priorState := boardProtocolValue(t, typ, prior)
		proposedState := boardProtocolValue(t, typ, proposed)
		config := boardProtocolValue(t, typ, map[string]tftypes.Value{"name": tfString("team-a"), "source_board_id": tfString("template")})
		resp, err := server.PlanResourceChange(ctx, &tfprotov6.PlanResourceChangeRequest{
			TypeName:         "homarr_board_copy",
			PriorState:       &priorState,
			ProposedNewState: &proposedState,
			Config:           &config,
		})
		if err != nil {
			t.Fatal(err)
		}
		requireNoProtocolErrors(t, resp.Diagnostics)
		return resp
	}

	if requiresReplaceOf(plan(tftypes.NewValue(tftypes.String, nil)), "source_board_id") {
		t.Fatal("recording source_board_id after an import planned a replacement")
	}
	if !requiresReplaceOf(plan(tfString("old-template")), "source_board_id") {
		t.Fatal("a new source board did not replace the copy")
	}
}

func addItemActionType(t *testing.T) tftypes.Type {
	t.Helper()
	var resp action.SchemaResponse
	(&boardAddItemAction{}).Schema(context.Background(), action.SchemaRequest{}, &resp)
	return resp.Schema.Type().TerraformType(context.Background())
}

func TestBoardAddItemActionSchemaIsServed(t *testing.T) {
	server := boardHarnessServer(t, newFakeHomarr())

	resp, err := server.GetProviderSchema(context.Background(), &tfprotov6.GetProviderSchemaRequest{})
	if err != nil {
		t.Fatal(err)
	}
	requireNoProtocolErrors(t, resp.Diagnostics)
	if _, ok := resp.ActionSchemas["homarr_board_add_item"]; !ok {
		t.Fatalf("homarr_board_add_item missing from the action schemas: %v", resp.ActionSchemas)
	}
	if _, ok := resp.ResourceSchemas["homarr_board_copy"]; !ok {
		t.Fatal("homarr_board_copy missing from the resource schemas")
	}
}

func TestBoardAddItemActionValidatesItsConfig(t *testing.T) {
	ctx := context.Background()
	server := boardHarnessServer(t, newFakeHomarr())
	typ := addItemActionType(t)

	validate := func(attrs map[string]tftypes.Value) []*tfprotov6.Diagnostic {
		config := boardProtocolValue(t, typ, attrs)
		resp, err := server.ValidateActionConfig(ctx, &tfprotov6.ValidateActionConfigRequest{
			ActionType: "homarr_board_add_item",
			Config:     &config,
		})
		if err != nil {
			t.Fatal(err)
		}
		return resp.Diagnostics
	}

	requireNoProtocolErrors(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("app"), "options_json": tfString(`{"appId":"a1"}`),
		"width": tfNumber(2), "height": tfNumber(1),
	}))
	requireProtocolErrorContaining(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(2),
	}), "height")
	requireProtocolErrorContaining(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "options_json": tfString(`["not", "an", "object"]`),
	}), "must be a JSON object")
	requireProtocolErrorContaining(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(30), "height": tfNumber(1),
	}), "between 1 and 24")

	// A position needs a layout, both offsets and the size.
	requireNoProtocolErrors(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(2), "height": tfNumber(1),
		"layout_id": tfString("l1"), "x": tfNumber(4), "y": tfNumber(0),
	}))
	requireProtocolErrorContaining(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(2), "height": tfNumber(1),
		"x": tfNumber(4), "y": tfNumber(0),
	}), "layout_id")
	requireProtocolErrorContaining(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"),
		"layout_id": tfString("l1"), "x": tfNumber(4), "y": tfNumber(0),
	}), "width")
	// An unknown layout id, as homarr_board.base_layout_id is while the board
	// is still to be created, passes validation.
	requireNoProtocolErrors(t, validate(map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("clock"), "width": tfNumber(2), "height": tfNumber(1),
		"layout_id": tftypes.NewValue(tftypes.String, tftypes.UnknownValue), "x": tfNumber(4), "y": tfNumber(0),
	}))
}

func TestBoardAddItemActionInvokesThroughTheServer(t *testing.T) {
	ctx := context.Background()
	fake := newFakeHomarr()
	fake.reply("POST /api/boards/items", 200, `{"itemId":"i1"}`)
	server := boardHarnessServer(t, fake)

	config := boardProtocolValue(t, addItemActionType(t), map[string]tftypes.Value{
		"board_id": tfString("b1"), "kind": tfString("app"), "options_json": tfString(`{"appId":"a1"}`),
		"integration_ids": tftypes.NewValue(tftypes.List{ElementType: tftypes.String}, []tftypes.Value{tfString("int1")}),
		"width":           tfNumber(2), "height": tfNumber(1),
	})
	stream, err := server.InvokeAction(ctx, &tfprotov6.InvokeActionRequest{ActionType: "homarr_board_add_item", Config: &config})
	if err != nil {
		t.Fatal(err)
	}

	var progress []string
	completed := false
	for event := range stream.Events {
		switch e := event.Type.(type) {
		case tfprotov6.ProgressInvokeActionEventType:
			progress = append(progress, e.Message)
		case tfprotov6.CompletedInvokeActionEventType:
			requireNoProtocolErrors(t, e.Diagnostics)
			completed = true
		}
	}
	if !completed {
		t.Fatal("the invocation never completed")
	}
	if len(progress) != 1 || !strings.Contains(progress[0], "i1") {
		t.Fatalf("progress = %v, want one message naming the new item i1", progress)
	}

	body := fake.body(t, "POST /api/boards/items")
	options, _ := body["options"].(map[string]any)
	size, _ := body["size"].(map[string]any)
	ids, _ := body["integrationIds"].([]any)
	if body["boardId"] != "b1" || body["kind"] != "app" || options["appId"] != "a1" ||
		size["width"] != float64(2) || len(ids) != 1 || ids[0] != "int1" {
		t.Fatalf("POST /api/boards/items body = %v", body)
	}
}
