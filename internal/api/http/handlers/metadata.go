package handlers

import (
	"net/http"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// safeMetadata is a sanitised view of the VM metadata with the password omitted.
type safeMetadata struct {
	Hostname            string                                    `json:"hostname,omitempty"`
	Username            string                                    `json:"username,omitempty"`
	VirtualMachineID    string                                    `json:"virtual_machine_id,omitempty"`
	VirtualDisks        *isoconfig.DataList[isoconfig.VirtualDisk]         `json:"virtual_disks,omitempty"`
	VirtualNetworkCards *isoconfig.DataList[isoconfig.VirtualNetworkCard]  `json:"virtual_network_cards,omitempty"`
	ServiceRoles        *isoconfig.DataList[isoconfig.ServiceRole]         `json:"service_roles,omitempty"`
	SSHPublicKeys       []string                                  `json:"ssh_public_keys,omitempty"`
}

// MetadataHandler wires the ISO metadata to HTTP endpoints.
type MetadataHandler struct {
	iso *isoconfig.ISOMetadata
}

// NewMetadataHandler creates a MetadataHandler backed by the given ISO metadata.
func NewMetadataHandler(iso *isoconfig.ISOMetadata) *MetadataHandler {
	return &MetadataHandler{iso: iso}
}

// Show handles GET /api/v1/metadata.
// Returns the full ISO metadata excluding the sensitive password field.
func (h *MetadataHandler) Show(w http.ResponseWriter, r *http.Request) {
	raw := h.iso.Raw()
	if raw == nil {
		response.Error(w, http.StatusServiceUnavailable, "METADATA_UNAVAILABLE",
			"ISO metadata is not available. Ensure the config drive is mounted.")
		return
	}
	response.Success(w, safeMetadata{
		Hostname:            raw.Hostname,
		Username:            raw.Username,
		VirtualMachineID:    raw.VirtualMachineID,
		VirtualDisks:        &raw.VirtualDisks,
		VirtualNetworkCards: &raw.VirtualNetworkCards,
		ServiceRoles:        &raw.ServiceRoles,
		SSHPublicKeys:       raw.SSHPublicKeys,
	})
}

// GetInstance handles GET /api/v1/metadata/instance.
// Returns VM identity and disk information.
func (h *MetadataHandler) GetInstance(w http.ResponseWriter, r *http.Request) {
	raw := h.iso.Raw()
	if raw == nil {
		response.Error(w, http.StatusNotFound, "INSTANCE_METADATA_NOT_FOUND",
			"Instance metadata is not available.")
		return
	}
	response.Success(w, struct {
		Hostname         string                                   `json:"hostname"`
		VirtualMachineID string                                   `json:"virtual_machine_id"`
		Username         string                                   `json:"username"`
		VirtualDisks     isoconfig.DataList[isoconfig.VirtualDisk] `json:"virtual_disks"`
		SSHPublicKeys    []string                                 `json:"ssh_public_keys"`
	}{
		Hostname:         raw.Hostname,
		VirtualMachineID: raw.VirtualMachineID,
		Username:         raw.Username,
		VirtualDisks:     raw.VirtualDisks,
		SSHPublicKeys:    raw.SSHPublicKeys,
	})
}

// GetNetwork handles GET /api/v1/metadata/network.
// Returns virtual network card and IP assignment information.
func (h *MetadataHandler) GetNetwork(w http.ResponseWriter, r *http.Request) {
	raw := h.iso.Raw()
	if raw == nil || len(raw.VirtualNetworkCards.Data) == 0 {
		response.Error(w, http.StatusNotFound, "NETWORK_METADATA_NOT_FOUND",
			"Network metadata is not available.")
		return
	}
	response.Success(w, raw.VirtualNetworkCards)
}

// GetServices handles GET /api/v1/metadata/services.
// Returns the service roles assigned to this VM.
func (h *MetadataHandler) GetServices(w http.ResponseWriter, r *http.Request) {
	raw := h.iso.Raw()
	if raw == nil {
		response.Error(w, http.StatusNotFound, "SERVICES_METADATA_NOT_FOUND",
			"Services metadata is not available.")
		return
	}
	response.Success(w, raw.ServiceRoles)
}
