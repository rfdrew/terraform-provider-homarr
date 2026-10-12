package provider

import (
	"bytes"
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/hashicorp/terraform-plugin-framework/datasource/schema"
	"github.com/hashicorp/terraform-plugin-framework/path"
	"github.com/hashicorp/terraform-plugin-framework/schema/validator"
	"github.com/hashicorp/terraform-plugin-framework/types"
	"github.com/hashicorp/terraform-plugin-testing/helper/resource"
)

const rootCertificateRoute = "GET /api/certificates/corp-root-ca.crt"

func rootCertificateConfig(fileName string) *rootCertificateDataSourceModel {
	return &rootCertificateDataSourceModel{
		FileName:       types.StringValue(fileName),
		Found:          types.BoolNull(),
		SHA256:         types.StringNull(),
		Fingerprint256: types.StringNull(),
		CA:             types.BoolNull(),
		ValidFrom:      types.StringNull(),
		ValidTo:        types.StringNull(),
	}
}

func TestRootCertificateDataSourceReportsAFoundCertificate(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(rootCertificateRoute, 200, `{"fileName":"corp-root-ca.crt",`+
		`"sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",`+
		`"fingerprint256":"AB:CD:EF","ca":true,`+
		`"validFrom":"2025-01-01T00:00:00.000Z","validTo":"2035-06-30T12:00:00.000Z"}`)
	d := &rootCertificateDataSource{client: fake.client(t)}

	var got rootCertificateDataSourceModel
	requireNoErrors(t, readDataSourceForTest(t, d, rootCertificateConfig("corp-root-ca.crt"), &got))

	if !got.Found.ValueBool() || !got.CA.ValueBool() || got.Fingerprint256.ValueString() != "AB:CD:EF" ||
		got.SHA256.ValueString() != "9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08" {
		t.Fatalf("certificate = %+v, want the metadata Homarr returned", got)
	}
	// Homarr prints JavaScript's toISOString; the data source reports RFC 3339
	// in UTC without the zero fraction, as homarr_invite does.
	if got.ValidFrom.ValueString() != "2025-01-01T00:00:00Z" || got.ValidTo.ValueString() != "2035-06-30T12:00:00Z" {
		t.Fatalf("valid_from = %v, valid_to = %v; want RFC 3339 in UTC", got.ValidFrom, got.ValidTo)
	}
}

// A missing file is a 200 with null, and is reported as found = false rather
// than as an error, so a check block can test for it.
func TestRootCertificateDataSourceReportsAMissingFileAsNotFound(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(rootCertificateRoute, 200, `null`)
	d := &rootCertificateDataSource{client: fake.client(t)}

	var got rootCertificateDataSourceModel
	requireNoErrors(t, readDataSourceForTest(t, d, rootCertificateConfig("corp-root-ca.crt"), &got))

	if got.Found.IsNull() || got.Found.ValueBool() {
		t.Fatalf("found = %v, want false", got.Found)
	}
	for name, v := range map[string]interface{ IsNull() bool }{
		"sha256": got.SHA256, "fingerprint256": got.Fingerprint256, "ca": got.CA,
		"valid_from": got.ValidFrom, "valid_to": got.ValidTo,
	} {
		if !v.IsNull() {
			t.Errorf("%s = %v, want null for a missing certificate", name, v)
		}
	}
}

func TestRootCertificateDataSourceBlamesA404OnTheHomarrVersion(t *testing.T) {
	fake := newFakeHomarr() // the certificate route is unrouted, so 404
	fake.reply("GET /api/info", 200, `{"version":"2.1.3"}`)
	d := &rootCertificateDataSource{client: fake.client(t)}

	var got rootCertificateDataSourceModel
	diags := readDataSourceForTest(t, d, rootCertificateConfig("corp-root-ca.crt"), &got)
	requireErrorContaining(t, diags, "Requires Homarr 2.2.0 or newer")
	requireErrorContaining(t, diags, "This Homarr reports version 2.1.3")
}

// Without a version, the 404 still means a missing route, but whether the
// release or a proxy is to blame cannot be told, so neither is asserted.
func TestRootCertificateDataSourceHedgesA404WithoutAVersion(t *testing.T) {
	for _, tc := range []struct{ name, info string }{
		{"no answer", ``},
		{"no release version", `{"version":"custom"}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			fake := newFakeHomarr() // the certificate route is unrouted, so 404
			if tc.info != "" {
				fake.reply("GET /api/info", 200, tc.info)
			}
			d := &rootCertificateDataSource{client: fake.client(t)}

			var got rootCertificateDataSourceModel
			diags := readDataSourceForTest(t, d, rootCertificateConfig("corp-root-ca.crt"), &got)
			requireErrorContaining(t, diags, "predates 2.2.0")
			requireErrorContaining(t, diags, "proxy")
			for _, e := range diags.Errors() {
				if strings.Contains(e.Summary(), "Requires Homarr") {
					t.Fatalf("a Homarr of unknown version was told to upgrade: %v", diags)
				}
			}
		})
	}
}

// A Homarr new enough to have the route cannot be the reason for its 404, so
// the version is not blamed.
func TestRootCertificateDataSourceDoesNotBlameARecentHomarrFor404(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply("GET /api/info", 200, `{"version":"2.4.1"}`)
	d := &rootCertificateDataSource{client: fake.client(t)}

	var got rootCertificateDataSourceModel
	diags := readDataSourceForTest(t, d, rootCertificateConfig("corp-root-ca.crt"), &got)
	requireErrorContaining(t, diags, "reports version 2.4.1, which has GET /api/certificates/{fileName}")
	for _, e := range diags.Errors() {
		if strings.Contains(e.Summary(), "Requires Homarr") {
			t.Fatalf("a 404 from Homarr 2.4.1 was blamed on its version: %v", diags)
		}
	}
}

func TestRootCertificateDataSourcePassesOtherErrorsThrough(t *testing.T) {
	fake := newFakeHomarr()
	fake.reply(rootCertificateRoute, 400, `{"message":"Expected a self-signed root CA certificate","code":"BAD_REQUEST"}`)
	d := &rootCertificateDataSource{client: fake.client(t)}

	var got rootCertificateDataSourceModel
	diags := readDataSourceForTest(t, d, rootCertificateConfig("corp-root-ca.crt"), &got)
	requireErrorContaining(t, diags, "Expected a self-signed root CA certificate")
	if fake.called("GET /api/info") {
		t.Fatal("asked for the version although the error was not a 404")
	}
}

func TestRootCertificateFileNameValidatorMatchesHomarr(t *testing.T) {
	attr, ok := dataSourceSchemaForTest(t, &rootCertificateDataSource{}).Attributes["file_name"].(schema.StringAttribute)
	if !ok {
		t.Fatal("file_name is not a string attribute")
	}
	passes := func(value string) bool {
		for _, v := range attr.Validators {
			var resp validator.StringResponse
			v.ValidateString(context.Background(), validator.StringRequest{
				Path:        path.Root("file_name"),
				ConfigValue: types.StringValue(value),
			}, &resp)
			if resp.Diagnostics.HasError() {
				return false
			}
		}
		return true
	}

	for value, want := range map[string]bool{
		"corp-root-ca.crt":                true,
		"ca.pem":                          true,
		"ROOT.pem":                        true,
		"0_Root.CA-2027.crt":              true,
		"a..crt":                          true,
		"a.crt.pem":                       true,
		strings.Repeat("a", 124) + ".crt": true,
		strings.Repeat("a", 125) + ".crt": false, // 129 characters
		".hidden.crt":                     false,
		"-ca.crt":                         false,
		"..crt":                           false,
		"ca.der":                          false,
		"ca.CRT":                          false, // the extension is matched case-sensitively
		"ca.crt.bak":                      false,
		"certs/ca.crt":                    false,
		"corp root.crt":                   false,
		"ca.crt\n":                        false,
		"\u0441a.crt":                     false, // a Cyrillic es
		"":                                false,
	} {
		if got := passes(value); got != want {
			t.Errorf("file_name = %q passes = %v, want %v", value, got, want)
		}
	}
}

// --- acceptance ---------------------------------------------------------------

func TestAccRootCertificateDataSource_missing(t *testing.T) {
	resource.Test(t, resource.TestCase{
		PreCheck:                 func() { testAccPreCheck(t) },
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		// The invalid name goes first: the post-test destroy plans the last
		// step's configuration, and one that fails validation would fail it.
		Steps: []resource.TestStep{
			{
				// Refused at plan time, before Homarr would answer 400.
				Config: providerConfig + `
data "homarr_root_certificate" "invalid" {
  file_name = "certs/root.der"
}
`,
				ExpectError: regexpMustCompile(`must start with a letter or digit`),
			},
			{
				Config: providerConfig + `
data "homarr_root_certificate" "missing" {
  file_name = "tfacc-does-not-exist.crt"
}
`,
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.homarr_root_certificate.missing", "found", "false"),
					resource.TestCheckNoResourceAttr("data.homarr_root_certificate.missing", "sha256"),
					resource.TestCheckNoResourceAttr("data.homarr_root_certificate.missing", "valid_to"),
				),
			},
		},
	})
}

// TestAccRootCertificateDataSource_found adds a freshly generated root CA
// through POST /api/certificates/root, which the provider does not otherwise
// use, and reads it back. Homarr has no endpoint to remove a certificate, so
// the file stays in the test instance; a random name keeps reruns apart.
func TestAccRootCertificateDataSource_found(t *testing.T) {
	ca := newTestAccRootCA(t)
	resource.Test(t, resource.TestCase{
		PreCheck: func() {
			testAccPreCheck(t)
			ca.upload(t)
		},
		ProtoV6ProviderFactories: testAccProtoV6ProviderFactories,
		Steps: []resource.TestStep{
			{
				Config: providerConfig + fmt.Sprintf(`
data "homarr_root_certificate" "test" {
  file_name = %q
}
`, ca.fileName),
				Check: resource.ComposeAggregateTestCheckFunc(
					resource.TestCheckResourceAttr("data.homarr_root_certificate.test", "found", "true"),
					resource.TestCheckResourceAttr("data.homarr_root_certificate.test", "ca", "true"),
					resource.TestCheckResourceAttr("data.homarr_root_certificate.test", "sha256", ca.sha256),
					resource.TestCheckResourceAttr("data.homarr_root_certificate.test", "fingerprint256", ca.fingerprint256),
					resource.TestCheckResourceAttr("data.homarr_root_certificate.test", "valid_from", ca.validFrom),
					resource.TestCheckResourceAttr("data.homarr_root_certificate.test", "valid_to", ca.validTo),
				),
			},
		},
	})
}

// testAccRootCA is a self-signed root CA generated for one test run, with the
// metadata Homarr is expected to report for it.
type testAccRootCA struct {
	fileName       string
	pem            []byte
	sha256         string
	fingerprint256 string
	validFrom      string
	validTo        string
}

// newTestAccRootCA generates the CA locally, so a skipped test costs nothing.
func newTestAccRootCA(t *testing.T) testAccRootCA {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	serial, err := rand.Int(rand.Reader, new(big.Int).Lsh(big.NewInt(1), 62))
	if err != nil {
		t.Fatal(err)
	}
	// Homarr only accepts a certificate that is valid right now.
	now := time.Now().UTC().Truncate(time.Second)
	template := &x509.Certificate{
		SerialNumber:          serial,
		Subject:               pkix.Name{CommonName: "terraform-provider-homarr acceptance test CA"},
		NotBefore:             now.Add(-time.Hour),
		NotAfter:              now.Add(365 * 24 * time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageCRLSign,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certPEM := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})

	// Node prints the fingerprint as upper-case hex pairs joined by colons.
	derSum := sha256.Sum256(der)
	pairs := make([]string, len(derSum))
	for i, b := range derSum {
		pairs[i] = strings.ToUpper(hex.EncodeToString([]byte{b}))
	}
	pemSum := sha256.Sum256(certPEM)

	return testAccRootCA{
		fileName:       fmt.Sprintf("tfacc-%d.crt", serial.Int64()),
		pem:            certPEM,
		sha256:         hex.EncodeToString(pemSum[:]),
		fingerprint256: strings.Join(pairs, ":"),
		validFrom:      template.NotBefore.Format(time.RFC3339),
		validTo:        template.NotAfter.Format(time.RFC3339),
	}
}

// upload adds the CA to the Homarr named by HOMARR_URL and HOMARR_API_KEY.
func (ca testAccRootCA) upload(t *testing.T) {
	t.Helper()
	body, err := json.Marshal(map[string]string{"fileName": ca.fileName, "certificate": string(ca.pem)})
	if err != nil {
		t.Fatal(err)
	}
	base := strings.TrimSuffix(strings.TrimRight(os.Getenv("HOMARR_URL"), "/"), "/api")
	req, err := http.NewRequest(http.MethodPost, base+"/api/certificates/root", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set("ApiKey", os.Getenv("HOMARR_API_KEY"))
	req.Header.Set("Content-Type", "application/json")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		var answer bytes.Buffer
		_, _ = answer.ReadFrom(resp.Body)
		t.Fatalf("adding the test CA to Homarr answered HTTP %d: %s", resp.StatusCode, answer.String())
	}
}
