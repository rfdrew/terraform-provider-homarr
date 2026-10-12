package provider

import (
	"context"
	"regexp"
	"time"

	"github.com/hashicorp/terraform-plugin-framework-validators/stringvalidator"
	"github.com/hashicorp/terraform-plugin-framework/datasource"
	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"

	"github.com/rfdrew/terraform-provider-homarr/internal/client"
)

// rootCertificateRelease added GET /api/certificates/{fileName}.
var rootCertificateRelease = homarrRelease{2, 2, 0}

// rootCertificateFileNamePattern is rootCertificateFileNameSchema's regex.
// Homarr refuses any other name with a 400 before it looks at the disk, so the
// data source refuses it at plan time instead.
var rootCertificateFileNamePattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9._-]*\.(crt|pem)$`)

var (
	_ datasource.DataSource              = &rootCertificateDataSource{}
	_ datasource.DataSourceWithConfigure = &rootCertificateDataSource{}
)

// NewRootCertificateDataSource returns the homarr_root_certificate data source.
func NewRootCertificateDataSource() datasource.DataSource { return &rootCertificateDataSource{} }

type rootCertificateDataSource struct {
	client *client.Client
}

type rootCertificateDataSourceModel struct {
	FileName       types.String `tfsdk:"file_name"`
	Found          types.Bool   `tfsdk:"found"`
	SHA256         types.String `tfsdk:"sha256"`
	Fingerprint256 types.String `tfsdk:"fingerprint256"`
	CA             types.Bool   `tfsdk:"ca"`
	ValidFrom      types.String `tfsdk:"valid_from"`
	ValidTo        types.String `tfsdk:"valid_to"`
}

func (d *rootCertificateDataSource) Metadata(_ context.Context, req datasource.MetadataRequest, resp *datasource.MetadataResponse) {
	resp.TypeName = req.ProviderTypeName + "_root_certificate"
}

func (d *rootCertificateDataSource) Schema(_ context.Context, _ datasource.SchemaRequest, resp *datasource.SchemaResponse) {
	resp.Schema = schema.Schema{
		MarkdownDescription: "Inspects a custom root CA certificate that Homarr trusts for its integrations, by the " +
			"file name it has in Homarr's trusted-certificates folder. Useful in a `check` block, for example " +
			"to warn before the CA expires.\n\n" +
			"~> **Requires Homarr 2.2.0 or newer**, which added `GET /api/certificates/{fileName}`, and an admin " +
			"API key.\n\n" +
			"A file Homarr does not have is not an error: `found` is `false` and the other attributes are null. " +
			"Homarr has no endpoint that lists certificates, so a file can only be looked up by its exact name. " +
			"A file that exists but is not a single self-signed root CA — a chain or a leaf certificate added " +
			"through the UI, for example — fails the read, as does a symlink or a file over 64 KiB. A Homarr " +
			"started outside production mode without `LOCAL_CERTIFICATE_PATH` has no certificate folder and " +
			"reports every file as not found.\n\n" +
			"The certificate itself is not managed here: Homarr's REST API can add one but cannot remove it, so " +
			"a resource could not be destroyed.",
		Attributes: map[string]schema.Attribute{
			"file_name": schema.StringAttribute{
				Required: true,
				MarkdownDescription: "File name of the certificate in Homarr's trusted-certificates folder, such as " +
					"`corp-root-ca.crt`. Homarr accepts at most 128 characters: a letter or digit, then letters, " +
					"digits, `.`, `_` or `-`, ending in `.crt` or `.pem`.",
				Validators: []validator.String{
					stringvalidator.LengthAtMost(128),
					stringvalidator.RegexMatches(rootCertificateFileNamePattern,
						"must start with a letter or digit, contain only letters, digits, '.', '_' and '-', and end "+
							"in .crt or .pem, as Homarr requires"),
				},
			},
			"found": schema.BoolAttribute{
				Computed:            true,
				MarkdownDescription: "Whether Homarr has a certificate file with this name.",
			},
			"sha256": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "SHA-256 of the file's bytes as lower-case hex, e.g. to compare with " +
					"`sha256(file(\"corp-root-ca.crt\"))`. Null when `found` is `false`.",
			},
			"fingerprint256": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "SHA-256 fingerprint of the certificate, as upper-case hex pairs separated by " +
					"colons. Null when `found` is `false`.",
			},
			"ca": schema.BoolAttribute{
				Computed: true,
				MarkdownDescription: "Always `true` for a certificate Homarr reports, since it only reports root " +
					"CAs. Null when `found` is `false`.",
			},
			"valid_from": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "Start of the certificate's validity, as an RFC 3339 timestamp in UTC. Null " +
					"when `found` is `false`.",
			},
			"valid_to": schema.StringAttribute{
				Computed: true,
				MarkdownDescription: "End of the certificate's validity, as an RFC 3339 timestamp in UTC; compare " +
					"it with `timecmp`. An expired certificate is still reported. Null when `found` is `false`.",
			},
		},
	}
}

func (d *rootCertificateDataSource) Configure(_ context.Context, req datasource.ConfigureRequest, resp *datasource.ConfigureResponse) {
	d.client = clientFromProviderData(req.ProviderData, &resp.Diagnostics)
}

// fromAPI fills the model from Homarr's answer, where nil means no such file.
func (m *rootCertificateDataSourceModel) fromAPI(certificate *client.RootCertificate) {
	if certificate == nil {
		m.Found = types.BoolValue(false)
		m.SHA256 = types.StringNull()
		m.Fingerprint256 = types.StringNull()
		m.CA = types.BoolNull()
		m.ValidFrom = types.StringNull()
		m.ValidTo = types.StringNull()
		return
	}
	m.Found = types.BoolValue(true)
	m.SHA256 = types.StringValue(certificate.SHA256)
	m.Fingerprint256 = types.StringValue(certificate.Fingerprint256)
	m.CA = types.BoolValue(certificate.CA)
	m.ValidFrom = rfc3339UTC(certificate.ValidFrom)
	m.ValidTo = rfc3339UTC(certificate.ValidTo)
}

// rfc3339UTC re-serialises a timestamp Homarr printed with JavaScript's
// toISOString, such as 2027-01-01T00:00:00.000Z, in RFC 3339 and UTC without a
// zero fraction — 2027-01-01T00:00:00Z, the way homarr_invite reports dates. A
// value that does not parse is passed through unchanged.
func rfc3339UTC(s string) types.String {
	parsed, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return types.StringValue(s)
	}
	return types.StringValue(parsed.UTC().Format(time.RFC3339Nano))
}

func (d *rootCertificateDataSource) Read(ctx context.Context, req datasource.ReadRequest, resp *datasource.ReadResponse) {
	var config rootCertificateDataSourceModel
	resp.Diagnostics.Append(req.Config.Get(ctx, &config)...)
	if resp.Diagnostics.HasError() {
		return
	}

	certificate, err := d.client.GetRootCertificate(ctx, config.FileName.ValueString())
	if err != nil {
		// Homarr answers a missing file with null, never with 404, so a 404
		// means that the route itself is missing — unless Homarr reports a
		// release that has it, when something in between answered instead.
		if client.IsNotFound(err) {
			older, known, version := homarrPredates(ctx, d.client, rootCertificateRelease)
			switch {
			case older:
				resp.Diagnostics.AddError(
					requiresHomarrSummary(rootCertificateRelease),
					requiresHomarrDetail("homarr_root_certificate", "GET /api/certificates/{fileName}",
						rootCertificateRelease, version),
				)
			case known:
				resp.Diagnostics.AddError(
					"Unable to read Homarr root certificate",
					"Homarr reports version "+version+", which has GET /api/certificates/{fileName}, yet the "+
						"request answered 404. Check that the provider's url reaches Homarr itself rather than a "+
						"proxy that rewrites /api paths. "+err.Error(),
				)
			default:
				// Without a version, either explanation is possible.
				resp.Diagnostics.AddError(
					"Unable to read Homarr root certificate",
					"GET /api/certificates/{fileName} answered 404, which Homarr never does for a missing file, "+
						"so the route itself was not found. Either this Homarr predates 2.2.0, which added it, "+
						"or the provider's url reaches a proxy that rewrites /api paths rather than Homarr "+
						"itself. Homarr's version could not be determined to tell the two apart. "+err.Error(),
				)
			}
			return
		}
		resp.Diagnostics.AddError("Unable to read Homarr root certificate", err.Error())
		return
	}

	config.fromAPI(certificate)
	resp.Diagnostics.Append(resp.State.Set(ctx, &config)...)
}
