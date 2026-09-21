// Package provider defines how external IoT providers plug into the
// connector. The design mirrors the Android app's Detector seam: one small
// contract, adapters in their own packages, and all provider-specific wire
// formats and edge cases contained inside the adapter that owns them.
//
// Not every provider can do everything — a webhook-driven provider has
// nothing to poll, and some providers accept no commands — so capabilities
// are expressed as separate optional interfaces rather than one wide
// interface full of "not supported" stubs. The polling loop and the command
// dispatcher check for the capability they need.
package provider

import (
	"context"
	"time"

	"github.com/poune-mehdipour/smart-eyes/cloud-connector/internal/domain"
)

// Provider is the minimal contract every integration satisfies.
type Provider interface {
	Name() domain.ProviderName
}

// DevicePoller lists the provider's devices, already normalized. ExternalID
// and Provider must be set; the connector assigns its own IDs on upsert.
type DevicePoller interface {
	Provider
	FetchDevices(ctx context.Context) ([]domain.Device, error)
}

// EventPoller fetches hazard events confirmed since a given time, already
// normalized (device identified by ExternalDeviceID in NormalizedEvent).
type EventPoller interface {
	Provider
	FetchEvents(ctx context.Context, since time.Time) ([]NormalizedEvent, error)
}

// CommandSender delivers a command to a device owned by this provider.
// Implementations translate the normalized command into whatever the
// provider's API expects and report the resulting state.
type CommandSender interface {
	Provider
	SendCommand(ctx context.Context, externalDeviceID string, cmd domain.Command) (domain.CommandState, string, error)
}

// NormalizedEvent is a hazard event as an adapter hands it over: fully
// normalized, but the device is still named by the provider's own ID because
// only the storage layer can resolve it to a connector device UUID.
type NormalizedEvent struct {
	ExternalDeviceID string
	Event            domain.HazardEvent // DeviceID left empty; filled after resolution
}
