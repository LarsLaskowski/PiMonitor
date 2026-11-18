package alert

import "time"

// Metric names of the openHAB alerts.
const (
	// MetricOpenHABDown is the alert metric of an installed openHAB that is
	// not reachable.
	MetricOpenHABDown = "openhab_down"
	// MetricOpenHABRSSPercent is the alert metric of the openHAB process's
	// resident memory as a percentage of the host's memory.
	MetricOpenHABRSSPercent = "openhab_rss_percent"
	// MetricOpenHABThingsOffline is the alert metric of the number of
	// openHAB things that are not online.
	MetricOpenHABThingsOffline = "openhab_things_offline"
)

// OpenHABThing is one openHAB thing that is not online.
type OpenHABThing struct {
	UID          string `json:"uid"`
	Label        string `json:"label"`
	Status       string `json:"status"`
	StatusDetail string `json:"status_detail"`
}

// OpenHABSample is the openHAB part of one snapshot, as the alert engine
// evaluates it.
type OpenHABSample struct {
	Timestamp     time.Time
	Installed     bool
	Down          bool
	RSSPercent    float64
	RSSValid      bool
	ThingsOffline int
	ThingsValid   bool
	Things        []OpenHABThing
}

// EvaluateOpenHAB folds one openHAB sample into the engine's state and returns
// the transition events it caused.
func (e *Engine) EvaluateOpenHAB(s OpenHABSample) []Event {
	return nil
}
