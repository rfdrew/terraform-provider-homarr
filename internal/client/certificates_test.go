package client

import (
	"context"
	"net/http"
	"testing"
)

func TestGetRootCertificateDecodesTheMetadata(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/certificates/corp-root-ca.crt", http.StatusOK, `{"fileName":"corp-root-ca.crt",`+
		`"sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",`+
		`"fingerprint256":"AB:CD:EF","ca":true,`+
		`"validFrom":"2025-01-01T00:00:00.000Z","validTo":"2035-01-01T00:00:00.000Z"}`)

	certificate, err := f.client(t).GetRootCertificate(context.Background(), "corp-root-ca.crt")
	if err != nil {
		t.Fatalf("GetRootCertificate() error = %v", err)
	}
	if certificate == nil || certificate.Fingerprint256 != "AB:CD:EF" || !certificate.CA ||
		certificate.ValidTo != "2035-01-01T00:00:00.000Z" {
		t.Fatalf("GetRootCertificate() = %+v, want the metadata Homarr returned", certificate)
	}
}

// Homarr answers a missing file with a 200 and a JSON null, not with a 404.
func TestGetRootCertificateReturnsNilForAMissingFile(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/certificates/absent.pem", http.StatusOK, `null`)

	certificate, err := f.client(t).GetRootCertificate(context.Background(), "absent.pem")
	if err != nil {
		t.Fatalf("GetRootCertificate() error = %v", err)
	}
	if certificate != nil {
		t.Fatalf("GetRootCertificate() = %+v, want nil for a file Homarr does not have", certificate)
	}
}

// Before Homarr 2.2.0 the route does not exist, and the REST handler answers
// its generic 404.
func TestGetRootCertificateReportsAMissingRouteAsNotFound(t *testing.T) {
	f := newFakeHomarr() // unrouted, so 404

	_, err := f.client(t).GetRootCertificate(context.Background(), "corp-root-ca.crt")
	if !IsNotFound(err) {
		t.Fatalf("GetRootCertificate() error = %v, want a 404", err)
	}
}
