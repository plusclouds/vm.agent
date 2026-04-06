package handlers

import (
	"net/http"

	"github.com/plusclouds/ubuntu-agent/internal/api/http/response"
	"github.com/plusclouds/ubuntu-agent/pkg/isoconfig"
)

// safeISOMetadata is a sanitised view of ISOMetadata with credentials omitted.
type safeISOMetadata struct {
	Instance *isoconfig.InstanceMetadata `json:"instance,omitempty"`
	Network  *isoconfig.NetworkMetadata  `json:"network,omitempty"`
	Services *isoconfig.ServicesManifest `json:"services,omitempty"`
	UserData string                      `json:"user_data,omitempty"`
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
// Returns the full ISO metadata excluding sensitive credentials.
func (h *MetadataHandler) Show(w http.ResponseWriter, r *http.Request) {
	if h.iso == nil {
		response.Error(w, http.StatusServiceUnavailable, "METADATA_UNAVAILABLE",
			"ISO metadata is not available. Ensure the config drive is mounted.")
		return
	}
	response.Success(w, safeISOMetadata{
		Instance: h.iso.Instance,
		Network:  h.iso.Network,
		Services: h.iso.Services,
		UserData: h.iso.UserData,
	})
}

// GetInstance handles GET /api/v1/metadata/instance.
// Returns VM identity information from instance.json on the ISO.
func (h *MetadataHandler) GetInstance(w http.ResponseWriter, r *http.Request) {
	if h.iso == nil || h.iso.Instance == nil {
		response.Error(w, http.StatusNotFound, "INSTANCE_METADATA_NOT_FOUND",
			"Instance metadata is not available.")
		return
	}
	response.Success(w, h.iso.Instance)
}

// GetNetwork handles GET /api/v1/metadata/network.
// Returns network configuration from network.json on the ISO.
func (h *MetadataHandler) GetNetwork(w http.ResponseWriter, r *http.Request) {
	if h.iso == nil || h.iso.Network == nil {
		response.Error(w, http.StatusNotFound, "NETWORK_METADATA_NOT_FOUND",
			"Network metadata is not available.")
		return
	}
	response.Success(w, h.iso.Network)
}

// GetServices handles GET /api/v1/metadata/services.
// Returns the services manifest from services.json on the ISO.
func (h *MetadataHandler) GetServices(w http.ResponseWriter, r *http.Request) {
	if h.iso == nil || h.iso.Services == nil {
		response.Error(w, http.StatusNotFound, "SERVICES_METADATA_NOT_FOUND",
			"Services metadata is not available.")
		return
	}
	response.Success(w, h.iso.Services)
}
