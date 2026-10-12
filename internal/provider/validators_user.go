package provider

import (
	"context"
	"fmt"
	"regexp"

	"github.com/hashicorp/terraform-plugin-framework/attr"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
)

// jsWhitespaceClass is the body of a character class matching what JavaScript's
// \s matches: the WhiteSpace and LineTerminator code points, which include every
// Unicode space separator (Zs) and U+FEFF. Go's \s is ASCII-only and leaves out
// \v besides, so the set has to be spelled out to agree with a zod pattern.
const jsWhitespaceClass = `\t\n\v\f\r \x{a0}\x{1680}\x{2000}-\x{200a}\x{2028}\x{2029}\x{202f}\x{205f}\x{3000}\x{feff}`

// emailPattern is zod 4's unicodeEmail, /^[^\s@"]{1,64}@[^\s@]{1,255}$/u, which
// every Homarr email schema uses. With the u flag JavaScript counts code points,
// as Go's regexp counts runes, so the repetition bounds carry over unchanged.
var emailPattern = regexp.MustCompile(`^[^` + jsWhitespaceClass + `@"]{1,64}@[^` + jsWhitespaceClass + `@]{1,255}$`)

// emailValidator rejects an address Homarr's email schema would answer with a
// 400. The check is deliberately as loose as Homarr's: it is about what Homarr
// accepts, not what can receive mail.
func emailValidator() validator.String {
	return stringCheck{
		description: `must be an email address as Homarr accepts it: 1 to 64 characters other than whitespace, ` +
			`"@" and '"', then "@", then 1 to 255 characters other than whitespace and "@"`,
		check: emailPattern.MatchString,
	}
}

// headerItemsValidator checks what Homarr validates across the zones of a
// header layout, which no single zone's validators can see: every item is
// unique over all three zones, and the `user` builtin is present somewhere.
// Homarr also requires at least one item, which the second rule implies.
//
// Items and zones that are not known yet are skipped. The presence of `user` is
// only judged once every zone is known, since an unknown zone may supply it.
type headerItemsValidator struct{}

func (headerItemsValidator) Description(_ context.Context) string {
	return "header items must be unique across the zones, and the builtin `user` must be among them"
}

func (v headerItemsValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (headerItemsValidator) ValidateObject(_ context.Context, req validator.ObjectRequest, resp *validator.ObjectResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	attrs := req.ConfigValue.Attributes()

	complete := true
	hasUser := false
	seen := map[string]string{}
	for _, zoneName := range headerZones {
		zone, ok := attrs[zoneName].(types.List)
		if !ok || zone.IsUnknown() {
			complete = false
			continue
		}
		for i, element := range zone.Elements() {
			key, known := headerItemKey(element)
			if !known {
				complete = false
				continue
			}
			if key == "" {
				// Neither or both of builtin and board_id: the item's own
				// validators report that.
				continue
			}
			where := fmt.Sprintf("%s[%d]", zoneName, i)
			if first, dup := seen[key]; dup {
				resp.Diagnostics.AddAttributeError(
					req.Path,
					"Duplicate header item",
					fmt.Sprintf("Attribute %s lists %s twice, at %s and at %s. Homarr requires every header item "+
						"to be unique across the three zones.", req.Path, key, first, where),
				)
				continue
			}
			seen[key] = where
			if key == "builtin:user" {
				hasUser = true
			}
		}
	}

	if complete && !hasUser {
		resp.Diagnostics.AddAttributeError(
			req.Path,
			"Header without the account menu",
			fmt.Sprintf("Attribute %s must include { builtin = \"user\" } in one of its zones. Homarr requires "+
				"the account menu to remain in the header, and refuses a header without any items.", req.Path),
		)
	}
}

// headerItemKey is Homarr's getHeaderItemKey for a configured item:
// "builtin:<id>" or "board:<id>". It reports false while the item is unknown,
// and returns "" for an item that sets neither or both fields.
func headerItemKey(v attr.Value) (string, bool) {
	object, ok := v.(types.Object)
	if !ok || object.IsUnknown() {
		return "", false
	}
	if object.IsNull() {
		return "", true
	}
	builtin, _ := object.Attributes()["builtin"].(types.String)
	board, _ := object.Attributes()["board_id"].(types.String)
	if builtin.IsUnknown() || board.IsUnknown() {
		return "", false
	}
	switch {
	case !builtin.IsNull() && board.IsNull():
		return "builtin:" + builtin.ValueString(), true
	case builtin.IsNull() && !board.IsNull():
		return "board:" + board.ValueString(), true
	}
	return "", true
}

// profileImagePattern is the pattern setProfileImage applies to a picture,
// /^data:image\/(png|jpeg|gif|webp);base64,[A-Za-z0-9/+]+=*$/. Its g flag is
// harmless there: zod resets lastIndex before every test.
var profileImagePattern = regexp.MustCompile(`^data:image/(png|jpeg|gif|webp);base64,[A-Za-z0-9/+]+=*$`)

// profileImageMaxLength is the .max(350000) setProfileImage puts on a picture.
// The pattern admits only ASCII, so a matching value's length in bytes is its
// length in UTF-16 code units too.
const profileImageMaxLength = 350000

// profileImageValidator admits what PUT /api/users/profileImage accepts, plus
// "", which asks for no picture. It is not a stringCheck because a rejected
// picture can be hundreds of kilobytes long: the error quotes only its start.
type profileImageValidator struct{}

func (profileImageValidator) Description(_ context.Context) string {
	return fmt.Sprintf("must be \"\" or a base64 data: URL of a PNG, JPEG, GIF or WebP image, at most %d "+
		"characters long", profileImageMaxLength)
}

func (v profileImageValidator) MarkdownDescription(ctx context.Context) string {
	return v.Description(ctx)
}

func (v profileImageValidator) ValidateString(ctx context.Context, req validator.StringRequest, resp *validator.StringResponse) {
	if req.ConfigValue.IsNull() || req.ConfigValue.IsUnknown() {
		return
	}
	image := req.ConfigValue.ValueString()
	if image == "" || (len(image) <= profileImageMaxLength && profileImagePattern.MatchString(image)) {
		return
	}
	start := image
	if runes := []rune(image); len(runes) > 64 {
		start = string(runes[:64]) + "…"
	}
	resp.Diagnostics.AddAttributeError(
		req.Path,
		"Invalid Attribute Value",
		fmt.Sprintf("Attribute %s %s, got %d characters starting with %q", req.Path, v.Description(ctx), jsLength(image), start),
	)
}
