package client

import (
	"context"
	"net/http"
	"testing"
)

func TestTestIntegrationConnectionDecodesBothOutcomes(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/integrations/ok/test-connection", http.StatusOK, `{"success":true}`)
	f.reply("GET /api/integrations/bad/test-connection", http.StatusOK, `{"success":false,"error":"certificate"}`)
	c := f.client(t)

	ok, err := c.TestIntegrationConnection(context.Background(), "ok")
	if err != nil {
		t.Fatalf("TestIntegrationConnection(ok) error = %v", err)
	}
	if !ok.Success || ok.Error != nil {
		t.Fatalf("TestIntegrationConnection(ok) = %+v, want success and no error", ok)
	}

	bad, err := c.TestIntegrationConnection(context.Background(), "bad")
	if err != nil {
		t.Fatalf("TestIntegrationConnection(bad) error = %v", err)
	}
	if bad.Success || bad.Error == nil || *bad.Error != "certificate" {
		t.Fatalf("TestIntegrationConnection(bad) = %+v, want a failure with the certificate category", bad)
	}
}

func TestTestIntegrationConnectionEscapesTheID(t *testing.T) {
	f := newFakeHomarr()
	f.reply("GET /api/integrations/a b/test-connection", http.StatusOK, `{"success":true}`)

	if _, err := f.client(t).TestIntegrationConnection(context.Background(), "a b"); err != nil {
		t.Fatalf("TestIntegrationConnection() error = %v", err)
	}
	if !f.called("GET /api/integrations/a b/test-connection") {
		t.Fatalf("calls = %v, want the id as one path segment", f.routes())
	}
}
