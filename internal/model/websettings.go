package model

import "time"

// DefaultName is the dashboard's name when the operator gives none.
const DefaultName = "Node"

// Where a dashboard setting comes from (story #61, decision 0004).
const (
	FromDefaultValue = "default"
	FromEnvironment  = "environment"
	FromWeb          = "web"
)

// WebSettings is the dashboard settings page (story #61): node-dash's own
// settings, each with where its value comes from. Never other environment
// variables.
type WebSettings struct {
	// Version identifies the files as read; a save with another one is
	// refused, so two tabs don't overwrite each other.
	Version string `json:"version"`
	// CanSave is false when node-dash can't write its state directory;
	// CantSave then says why. The values still show.
	CanSave  bool   `json:"canSave"`
	CantSave string `json:"cantSave,omitempty"`
	// Warnings are problems with the files, e.g. a broken display.json that
	// is ignored.
	Warnings []string `json:"warnings,omitempty"`
	Docker   bool     `json:"docker"`

	Name        WebText `json:"name"`
	Ancoro      WebBool `json:"ancoro"`
	UpdateCheck WebBool `json:"updateCheck"`

	// Switches are node control, changing settings and peer addresses, in
	// that order. They can only be turned off here.
	Switches []WebSwitch `json:"switches"`

	// Listen and Hosts are fixed at startup, and only shown.
	Listen WebText  `json:"listen"`
	Hosts  WebHosts `json:"hosts"`
	// Saved lists what a save changed, e.g. `Name "Node" → "My node"`; only
	// in the answer to a save.
	Saved []string `json:"saved,omitempty"`
}

// WebText is a text setting: its value and source, and what applies when
// the value set here is dropped ("Use that").
type WebText struct {
	Value          string `json:"value"`
	Source         string `json:"source"`
	Fallback       string `json:"fallback"`
	FallbackSource string `json:"fallbackSource"`
}

// WebBool is an on/off setting, like WebText.
type WebBool struct {
	Value          bool   `json:"value"`
	Source         string `json:"source"`
	Fallback       bool   `json:"fallback"`
	FallbackSource string `json:"fallbackSource"`
}

// WebHosts is NODEDASH_HOSTS.
type WebHosts struct {
	Value  []string `json:"value"`
	Source string   `json:"source"`
}

// WebSwitch is an access switch.
type WebSwitch struct {
	// Key is "control", "settings" or "peer-addresses".
	Key string `json:"key"`
	On  bool   `json:"on"`
	// EnvOn is whether the server turns it on; false: off on the server.
	EnvOn bool `json:"envOn"`
	// OffSince is when it was turned off here; nil when it wasn't.
	OffSince *time.Time `json:"offSince"`
	// Available is false for a switch the setup doesn't support, such as
	// node control in Docker.
	Available bool `json:"available"`
}
