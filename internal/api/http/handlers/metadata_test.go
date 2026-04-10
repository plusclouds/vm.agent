package handlers_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/handlers"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// sampleVM returns a populated VirtualMachineMetadata for use in handler tests.
func sampleVM() *isoconfig.VirtualMachineMetadata {
	gw := "185.255.175.254"
	return &isoconfig.VirtualMachineMetadata{
		Hostname:         "enelsa-s-r-v",
		Username:         "root",
		Password:         "s3cr3t",
		VirtualMachineID: "016c6a79-dbbe-4284-9ea0-75645ede8ca3",
		VirtualDisks: isoconfig.DataList[isoconfig.VirtualDisk]{
			Data: []isoconfig.VirtualDisk{
				{DiskType: "user", DeviceNumber: 0, TotalDisk: 85899345920},
			},
		},
		VirtualNetworkCards: isoconfig.DataList[isoconfig.VirtualNetworkCard]{
			Data: []isoconfig.VirtualNetworkCard{
				{
					DeviceNumber: 0,
					MACAddr:      "7a:9c:c0:d0:ff:bc",
					Network: isoconfig.DataWrapper[isoconfig.Network]{
						Data: isoconfig.Network{
							Gateway:        &gw,
							DNSNameservers: []string{"8.8.8.8/32"},
							MTU:            1500,
						},
					},
					IPList: isoconfig.DataList[isoconfig.IPEntry]{
						Data: []isoconfig.IPEntry{
							{ID: "ip-1", IPAddr: "185.255.172.129/32"},
						},
					},
				},
			},
		},
		ServiceRoles:  isoconfig.DataList[isoconfig.ServiceRole]{},
		SSHPublicKeys: []string{"ssh-rsa AAAA..."},
	}
}

// fullISOMeta is the shared test fixture.
var fullISOMeta = isoconfig.New(sampleVM())

// --- Show ---

func TestMetadataShow_Success(t *testing.T) {
	h := handlers.NewMetadataHandler(fullISOMeta)
	rec := httptest.NewRecorder()
	h.Show(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)
	if !env.Success {
		t.Error("expected success=true")
	}

	// Password must NOT appear in the response.
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["password"]; ok {
		t.Error("password must be omitted from Show response")
	}
	if _, ok := raw["hostname"]; !ok {
		t.Error("hostname should be present in Show response")
	}
}

func TestMetadataShow_NilRaw_Returns503(t *testing.T) {
	h := handlers.NewMetadataHandler(&isoconfig.ISOMetadata{})
	rec := httptest.NewRecorder()
	h.Show(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

func TestMetadataShow_NilISO_Returns503(t *testing.T) {
	h := handlers.NewMetadataHandler(nil)
	rec := httptest.NewRecorder()
	h.Show(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata", nil))
	if rec.Code != http.StatusServiceUnavailable {
		t.Errorf("expected 503, got %d", rec.Code)
	}
}

// --- GetInstance ---

func TestMetadataGetInstance_Success(t *testing.T) {
	h := handlers.NewMetadataHandler(fullISOMeta)
	rec := httptest.NewRecorder()
	h.GetInstance(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/instance", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)

	var obj map[string]json.RawMessage
	if err := json.Unmarshal(env.Data, &obj); err != nil {
		t.Fatal(err)
	}
	if _, ok := obj["virtual_machine_id"]; !ok {
		t.Error("virtual_machine_id should be present")
	}
	if _, ok := obj["password"]; ok {
		t.Error("password must not be in instance response")
	}
}

func TestMetadataGetInstance_NilRaw_Returns404(t *testing.T) {
	h := handlers.NewMetadataHandler(&isoconfig.ISOMetadata{})
	rec := httptest.NewRecorder()
	h.GetInstance(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/instance", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- GetNetwork ---

func TestMetadataGetNetwork_Success(t *testing.T) {
	h := handlers.NewMetadataHandler(fullISOMeta)
	rec := httptest.NewRecorder()
	h.GetNetwork(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/network", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
	env := decodeEnvelope(t, rec)

	var cards isoconfig.DataList[isoconfig.VirtualNetworkCard]
	if err := json.Unmarshal(env.Data, &cards); err != nil {
		t.Fatal(err)
	}
	if len(cards.Data) != 1 {
		t.Errorf("expected 1 NIC, got %d", len(cards.Data))
	}
	if cards.Data[0].MACAddr != "7a:9c:c0:d0:ff:bc" {
		t.Errorf("mac_addr: got %q", cards.Data[0].MACAddr)
	}
}

func TestMetadataGetNetwork_NoNICs_Returns404(t *testing.T) {
	vm := sampleVM()
	vm.VirtualNetworkCards.Data = nil
	h := handlers.NewMetadataHandler(isoconfig.New(vm))
	rec := httptest.NewRecorder()
	h.GetNetwork(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/network", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

func TestMetadataGetNetwork_NilRaw_Returns404(t *testing.T) {
	h := handlers.NewMetadataHandler(&isoconfig.ISOMetadata{})
	rec := httptest.NewRecorder()
	h.GetNetwork(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/network", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}

// --- GetServices ---

func TestMetadataGetServices_Success(t *testing.T) {
	vm := sampleVM()
	vm.ServiceRoles.Data = []isoconfig.ServiceRole{{Name: "nginx"}}
	h := handlers.NewMetadataHandler(isoconfig.New(vm))
	rec := httptest.NewRecorder()
	h.GetServices(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/services", nil))

	if rec.Code != http.StatusOK {
		t.Errorf("expected 200, got %d", rec.Code)
	}
}

func TestMetadataGetServices_NilRaw_Returns404(t *testing.T) {
	h := handlers.NewMetadataHandler(&isoconfig.ISOMetadata{})
	rec := httptest.NewRecorder()
	h.GetServices(rec, httptest.NewRequest(http.MethodGet, "/api/v1/metadata/services", nil))
	if rec.Code != http.StatusNotFound {
		t.Errorf("expected 404, got %d", rec.Code)
	}
}
