package provider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"sync"

	"github.com/hashicorp/terraform-plugin-framework/diag"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/resource"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-log/tflog"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// This file holds the board lifecycle steps that homarr_board and
// homarr_board_copy share: everything after a board exists is the same calls
// against the same endpoints, whichever way the board was made.

// homarrError unwraps the API error behind err, or returns nil for transport
// and decoding failures.
func homarrError(err error) *client.Error {
	var apiErr *client.Error
	if errors.As(err, &apiErr) {
		return apiErr
	}
	return nil
}

// fullAccessRequired explains a 404 from a board mutation while the board is
// still listed. Rename, visibility and delete need *full* access, and Homarr
// answers anyone without it with 404 so as not to reveal that the board
// exists — but a board GET /api/boards still lists is visible anyway, so the
// 404 can only mean the missing access. That typically hits a non-admin key
// acting on a board it did not create, such as an imported one.
func fullAccessRequired(id, doing string, err error) string {
	return fmt.Sprintf("Board %s is still listed for this API key, so Homarr's 404 means the key is not allowed "+
		"to do this: %s a board requires full access to it — being its creator, a \"full\" user or group "+
		"grant, or the board-full-all permission, which admins have. Homarr answers 404 rather than 403 to "+
		"anyone else. %s", id, doing, err.Error())
}

// explainBoardNotFound reports a 404 from an in-place board change. The board
// is re-read to tell a board deleted outside Terraform from one the key is not
// allowed to change.
func explainBoardNotFound(ctx context.Context, c *client.Client, id, summary, doing string, err error, diags *diag.Diagnostics) {
	_, getErr := c.GetBoard(ctx, id)
	switch {
	case getErr == nil:
		diags.AddError(summary, fullAccessRequired(id, doing, err))
	case client.IsNotFound(getErr):
		diags.AddError(summary, "Board "+id+" no longer exists; it was probably deleted outside Terraform. "+
			"Refresh to plan its re-creation. "+err.Error())
	default:
		diags.AddError(summary, err.Error()+". Checking whether the board still exists failed too: "+getErr.Error())
	}
}

// boardNameMu serialises the calls that give a board its name across every
// homarr_board and homarr_board_copy in a run: homarr_board's name check
// together with its create, and every copy and rename.
//
// Homarr's REST create checks no names, so homarr_board lists the boards and
// refuses a case variant itself before creating. Terraform creates independent
// resources in parallel, though, and without this lock two boards named "Ops"
// and "ops" in the same apply would both pass that check before either exists.
// Copies and renames are checked by Homarr, but only against the boards that
// exist when they run, so they could slip between another board's check and
// its create just the same.
//
// When both are held, boardNameMu is taken before homeBoardMu.
var boardNameMu sync.Mutex

// renameBoard applies a name change, reporting a taken name against the name
// attribute. It returns false when it added an error.
func renameBoard(ctx context.Context, c *client.Client, id, name string, diags *diag.Diagnostics) bool {
	boardNameMu.Lock()
	err := c.RenameBoard(ctx, id, name)
	boardNameMu.Unlock()
	switch {
	case err == nil:
		return true
	case client.IsConflict(err):
		diags.AddAttributeError(
			path.Root("name"),
			"Board name already taken",
			"Homarr requires board names to be unique, compared case-insensitively: "+err.Error(),
		)
	case client.IsNotFound(err):
		explainBoardNotFound(ctx, c, id, "Unable to rename Homarr board", "renaming", err, diags)
	default:
		diags.AddError("Unable to rename Homarr board", err.Error())
	}
	return false
}

// changeBoardVisibility makes a board public or private. It returns false when
// it added an error.
func changeBoardVisibility(ctx context.Context, c *client.Client, id string, isPublic bool, diags *diag.Diagnostics) bool {
	err := c.SetBoardVisibility(ctx, id, isPublic)
	switch {
	case err == nil:
		return true
	case client.IsNotFound(err):
		explainBoardNotFound(ctx, c, id, "Unable to change Homarr board visibility", "changing the visibility of", err, diags)
	case isInstanceHomeBoardRefusal(err):
		diags.AddAttributeError(
			path.Root("is_public"),
			"Unable to change Homarr board visibility",
			"Homarr rejects making a board private while it is the instance-wide desktop or mobile home "+
				"board, set as home_board_id or mobile_home_board_id in homarr_server_board_settings. "+err.Error(),
		)
	default:
		diags.AddError("Unable to change Homarr board visibility", err.Error())
	}
	return false
}

// isInstanceHomeBoardRefusal recognises Homarr's 400 "Cannot make home board
// private", the one visibility failure that is about the instance-wide desktop
// or mobile home board rather than access.
func isInstanceHomeBoardRefusal(err error) bool {
	apiErr := homarrError(err)
	return apiErr != nil && apiErr.StatusCode == http.StatusBadRequest &&
		strings.Contains(strings.ToLower(apiErr.Message), "home board")
}

// deleteBoard deletes a board for homarr_board and homarr_board_copy.
//
// A 404 is ambiguous: Homarr answers it for a board that is already gone and
// for one the key lacks full access to. The board is re-read before the 404 is
// taken as success, because treating a refusal as a deletion would drop a board
// that still exists from state.
func deleteBoard(ctx context.Context, c *client.Client, id string, diags *diag.Diagnostics) {
	if err := c.DeleteBoard(ctx, id); err != nil {
		if !client.IsNotFound(err) {
			diags.AddError("Unable to delete Homarr board", err.Error())
			return
		}
		_, getErr := c.GetBoard(ctx, id)
		switch {
		case getErr == nil:
			diags.AddError("Unable to delete Homarr board", fullAccessRequired(id, "deleting", err))
			return
		case !client.IsNotFound(getErr):
			diags.AddError(
				"Unable to verify the Homarr board was deleted",
				"Homarr answered 404 to the delete, and checking whether the board still exists failed: "+getErr.Error(),
			)
			return
		}
		// Not listed either: the board is gone, or invisible to this key, which
		// Read would treat as gone too.
	}
	warnIfInstanceHomeBoard(ctx, c, id, diags)
}

// warnIfInstanceHomeBoard warns when a deleted board is still referenced as an
// instance-wide home board.
//
// Homarr refuses to make such a board private but happily deletes it, and the
// server settings are a JSON blob with no foreign key, so the reference is left
// dangling. The check needs GET /api/settings/board, which is admin-only; any
// failure, 403 included, just skips the warning.
func warnIfInstanceHomeBoard(ctx context.Context, c *client.Client, id string, diags *diag.Diagnostics) {
	settings, err := c.GetServerBoardSettings(ctx)
	if err != nil {
		tflog.Debug(ctx, "skipping the instance-wide home board check", map[string]any{"error": err.Error()})
		return
	}

	var slots, attributes []string
	if settings.HomeBoardID != nil && *settings.HomeBoardID == id {
		slots, attributes = append(slots, "desktop"), append(attributes, "home_board_id")
	}
	if settings.MobileHomeBoardID != nil && *settings.MobileHomeBoardID == id {
		slots, attributes = append(slots, "mobile"), append(attributes, "mobile_home_board_id")
	}
	if len(slots) == 0 {
		return
	}

	diags.AddWarning(
		"Deleted board is still the instance-wide home board",
		fmt.Sprintf("Board %s was the instance-wide %s home board. Homarr does not clear that reference when a "+
			"board is deleted, so the server board settings now point at a board that no longer exists: visitors "+
			"without a home board of their own get a not-found page, and changes to those settings can be "+
			"refused while the stale id is stored. Set %s in homarr_server_board_settings to another public "+
			"board, or clear it with \"\" — if this configuration manages those settings, the same apply may "+
			"already do so.", id, strings.Join(slots, " and "), strings.Join(attributes, " and ")),
	)
}

// boardNameTakenOnCreate decides whether a failed POST /api/boards means the
// name is taken, returning the diagnostic detail if so.
//
// Homarr's REST create does not check names: an exact duplicate fails on the
// database's unique constraint and surfaces as HTTP 500, never the 409 rename
// gives. The constraint message identifies most of these; for any other 500
// the boards are re-listed, which catches a clash whose message the provider
// does not recognise, such as a future ORM wording.
func boardNameTakenOnCreate(ctx context.Context, c *client.Client, name string, err error) (string, bool) {
	if client.IsConflict(err) || client.IsBoardNameConstraintViolation(err) {
		return "Another board already uses this name — possibly one this API key cannot see, since GET " +
			"/api/boards lists only the boards the key may view. Homarr requires board names to be unique: " +
			err.Error(), true
	}
	if apiErr := homarrError(err); apiErr == nil || apiErr.StatusCode != http.StatusInternalServerError {
		return "", false
	}
	boards, listErr := c.ListBoards(ctx)
	if listErr != nil {
		return "", false
	}
	clash := client.BoardsWithSimilarName(boards, name)
	if len(clash) == 0 {
		return "", false
	}
	return fmt.Sprintf("Homarr failed with an internal error, and board %q (id %s) now exists. Board names "+
		"must be unique, compared case-insensitively, so this is the likely cause. If that board was not "+
		"there before, it may have been created by this request; import it rather than retrying. %s",
		clash[0].Name, clash[0].ID, err.Error()), true
}

// replacedBoardKey is the private state key under which ModifyPlan remembers
// the board an existing resource instance stands for.
const replacedBoardKey = "board_id"

// privateKeys is the part of the framework's private state that the name check
// uses. The concrete type lives in an internal framework package.
type privateKeys interface {
	GetKey(ctx context.Context, key string) ([]byte, diag.Diagnostics)
	SetKey(ctx context.Context, key string, value []byte) diag.Diagnostics
}

// modifyBoardNamePlan is the plan-time name check of homarr_board and
// homarr_board_copy. It warns about a name that another board visible to the
// key already uses, compared case-insensitively — the comparison rename and
// duplicate make, and the one POST /api/boards skips.
func modifyBoardNamePlan(ctx context.Context, c *client.Client, req resource.ModifyPlanRequest, resp *resource.ModifyPlanResponse) {
	// No client during early validation walks; nothing to check on destroy.
	if c == nil || req.Plan.Raw.IsNull() {
		return
	}

	var name, priorID, priorName types.String
	resp.Diagnostics.Append(req.Plan.GetAttribute(ctx, path.Root("name"), &name)...)
	if !req.State.Raw.IsNull() {
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("id"), &priorID)...)
		resp.Diagnostics.Append(req.State.GetAttribute(ctx, path.Root("name"), &priorName)...)
	}
	if resp.Diagnostics.HasError() {
		return
	}

	var private privateKeys
	if resp.Private != nil {
		private = resp.Private
	}
	checkPlannedBoardName(ctx, c, name, priorID, priorName, private, &resp.Diagnostics)
}

// checkPlannedBoardName runs the name check for one plan.
//
// It only ever warns, because a plan cannot tell a real clash from a board
// that gets out of the way earlier in the same apply. Two cases make that
// common enough to matter:
//
//   - Another resource renames or destroys the board holding the name first,
//     for instance a rename followed by a create that depends on it.
//   - A tainted object is replaced. Terraform plans its replacement exactly
//     like a fresh create, with a null prior state and no private state, so the
//     tainted board itself, usually under the very same name, reads as a clash.
//
// The real refusal comes at apply time: Homarr's rename and duplicate refuse a
// taken name with 409, an exact duplicate on create fails Homarr's unique
// constraint, and refuseCaseVariantBoardName covers the one gap Homarr leaves.
//
// A replacement the provider or -replace asked for is planned twice: once
// against the prior state, then again for the new object with a null prior
// state, passing along the private state of the first plan. So every plan
// against an existing board records that board's id in private state, and a
// plan without prior state ignores the board recorded there; such a
// replacement does not warn about its own board. Terraform currently keeps
// only the errors of that second plan, so the warning would not reach the user
// either way; the hand-over keeps the check correct without relying on that.
// A tainted object has no first plan, and therefore no such hand-over.
//
// The check is best effort: GET /api/boards lists only the boards the key may
// view, and a failure to list skips it rather than failing the plan.
func checkPlannedBoardName(ctx context.Context, c *client.Client, name, priorID, priorName types.String, private privateKeys, diags *diag.Diagnostics) {
	var ignoreID string
	if !priorID.IsNull() && !priorID.IsUnknown() {
		ignoreID = priorID.ValueString()
		if private != nil {
			encoded, _ := json.Marshal(ignoreID)
			diags.Append(private.SetKey(ctx, replacedBoardKey, encoded)...)
		}
		// Renames are checked; an unchanged name was fine when it was applied.
		if name.Equal(priorName) {
			return
		}
	} else if private != nil {
		raw, getDiags := private.GetKey(ctx, replacedBoardKey)
		diags.Append(getDiags...)
		if len(raw) > 0 {
			_ = json.Unmarshal(raw, &ignoreID)
			// The new object gets a different id; do not carry the old one into
			// its state.
			diags.Append(private.SetKey(ctx, replacedBoardKey, nil)...)
		}
	}

	if name.IsNull() || name.IsUnknown() {
		return
	}

	boards, err := c.ListBoards(ctx)
	if err != nil {
		tflog.Debug(ctx, "skipping the board name check", map[string]any{"error": err.Error()})
		return
	}
	clash := client.BoardsWithSimilarName(boards, name.ValueString(), ignoreID)
	if len(clash) == 0 {
		return
	}
	diags.AddAttributeWarning(
		path.Root("name"),
		"Board name already in use",
		fmt.Sprintf("Board %q (id %s) uses this name right now, and Homarr requires board names to be unique, "+
			"compared case-insensitively. Unless that board is renamed or destroyed earlier in the same apply, "+
			"the apply will fail. A plan cannot tell whether it is, so this is only a warning. Nothing needs "+
			"to be done when this plan replaces a tainted board of that name, which Terraform destroys first "+
			"— unless the resource uses create_before_destroy, which creates the replacement first and so "+
			"needs a different name.",
			clash[0].Name, clash[0].ID),
	)
}

// refuseCaseVariantBoardName is the name check homarr_board makes right before
// POST /api/boards, holding boardNameMu across both. It returns false when it
// added an error.
//
// Homarr's REST create checks no names: an exact duplicate fails on the
// database's unique constraint, which boardNameTakenOnCreate recognises, but a
// name differing only in case is accepted, although rename and duplicate would
// refuse it and the board's URL then reaches only one of the two boards. That
// gap is closed here, at apply time, when boards renamed or destroyed earlier
// in the apply are already out of the way. A failure to list skips the check.
func refuseCaseVariantBoardName(ctx context.Context, c *client.Client, name string, diags *diag.Diagnostics) bool {
	boards, err := c.ListBoards(ctx)
	if err != nil {
		tflog.Debug(ctx, "skipping the board name check before creation", map[string]any{"error": err.Error()})
		return true
	}
	for _, board := range client.BoardsWithSimilarName(boards, name) {
		if board.Name == name {
			// Left to Homarr, which refuses it for every board, visible or not.
			continue
		}
		diags.AddAttributeError(
			path.Root("name"),
			"Board name already taken",
			fmt.Sprintf("Board %q (id %s) already exists. Homarr requires board names to be unique, compared "+
				"case-insensitively, but its REST create does not check that: it would accept a name differing "+
				"only in case, although renaming or copying a board to such a name is refused, and the board's "+
				"URL would then reach only one of the two boards.", board.Name, board.ID),
		)
		return false
	}
	return true
}

// forgetReplacedBoard drops the id checkPlannedBoardName recorded, so that an
// in-place update does not leave it behind in the resource's private state.
func forgetReplacedBoard(ctx context.Context, private privateKeys, diags *diag.Diagnostics) {
	diags.Append(private.SetKey(ctx, replacedBoardKey, nil)...)
}

// importedBoardID resolves the board part of an import identifier: either a
// board id, used as is, or name=<name>, looked up through GET /api/boards with
// client.FindBoardByName's matching. Board ids are opaque and only shown in
// the address bar, while the name is what the UI shows everywhere.
func importedBoardID(ctx context.Context, c *client.Client, key string, diags *diag.Diagnostics) (string, bool) {
	name, byName := strings.CutPrefix(key, "name=")
	if !byName {
		return key, true
	}
	name = strings.TrimSpace(name)
	if name == "" {
		diags.AddError("Unexpected import identifier", "The name in \"name=<name>\" must not be empty.")
		return "", false
	}
	if c == nil {
		diags.AddError(
			"Unable to look up Homarr board",
			"Importing by name needs a configured provider to look the board up. Import by id instead.",
		)
		return "", false
	}

	board, err := c.GetBoardByName(ctx, name)
	if err != nil {
		var ambiguous *client.AmbiguousBoardNameError
		switch {
		case errors.As(err, &ambiguous):
			diags.AddError("Ambiguous board name", err.Error())
		case client.IsNotFound(err):
			diags.AddError(
				"Board not found",
				fmt.Sprintf("No board named %q is visible to this API key. GET /api/boards lists only the "+
					"boards the key may view.", name),
			)
		default:
			diags.AddError("Unable to look up Homarr board", err.Error())
		}
		return "", false
	}
	return board.ID, true
}
