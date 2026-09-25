package datasource

import "context"

// Actuator is implemented by datasources that can perform an outbound control
// action (a write) against their service. Handlers resolve a configured source
// and type-assert to Actuator, so only sources an admin has already configured
// can be actuated.
type Actuator interface {
	Actuate(ctx context.Context, action string, params map[string]string) error
}

// Control action names. Kept as strings so the API and scene/rule JSON stay
// human-readable and forward-compatible.
const (
	ActionCallService = "call_service" // homeassistant
	ActionPause       = "pause"        // qbittorrent
	ActionResume      = "resume"       // qbittorrent
	ActionApprove     = "approve"      // overseerr
	ActionRequest     = "request"      // genericapi
)

// controlActions maps a source type key to the actions it supports. Used by the
// control API to advertise capabilities without instantiating a source.
var controlActions = map[string][]string{
	"homeassistant": {ActionCallService},
	"qbittorrent":   {ActionPause, ActionResume},
	"overseerr":     {ActionApprove},
	"genericapi":    {ActionRequest},
}

// ControlActions returns the supported actions for a source type, or nil.
func ControlActions(sourceType string) []string {
	return controlActions[sourceType]
}
