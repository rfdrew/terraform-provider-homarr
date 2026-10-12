package client

import (
	"context"
	"net/url"
)

// RootCertificate is the metadata GET /api/certificates/{fileName} reports for
// a custom root CA in Homarr's trusted-certificates folder. Homarr never
// returns the PEM itself.
type RootCertificate struct {
	FileName string `json:"fileName"`
	// SHA256 is the lower-case hex SHA-256 of the file's bytes, as uploaded.
	SHA256 string `json:"sha256"`
	// Fingerprint256 is the certificate's SHA-256 fingerprint, upper-case hex
	// pairs separated by colons, as Node's X509Certificate prints it.
	Fingerprint256 string `json:"fingerprint256"`
	CA             bool   `json:"ca"`
	// ValidFrom and ValidTo are ISO 8601 timestamps in UTC with milliseconds,
	// such as 2027-01-01T00:00:00.000Z.
	ValidFrom string `json:"validFrom"`
	ValidTo   string `json:"validTo"`
}

// GetRootCertificate inspects a custom root CA by file name. It returns nil,
// and no error, when Homarr has no such file.
//
// Requires an admin API key and Homarr 2.2.0 or newer. The endpoint answers
// null rather than 404 for a missing file, so a 404 means the route itself is
// missing: an older Homarr. A file that exists but is not one self-signed root
// CA — a chain added through the UI, for example — answers 400, and a symlink,
// FIFO or file over 64 KiB answers 409.
func (c *Client) GetRootCertificate(ctx context.Context, fileName string) (*RootCertificate, error) {
	var certificate *RootCertificate
	if err := c.do(ctx, "GET", "/api/certificates/"+url.PathEscape(fileName), nil, &certificate); err != nil {
		return nil, err
	}
	return certificate, nil
}
