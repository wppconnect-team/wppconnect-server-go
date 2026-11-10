package eventbus

import "encoding/json"

// Workflow and broker consumers may observe that pairing is available, but do not
// need the live device-link credential. Project this on each delivery, including replay.
func externalIntegrationEvent(e Event) Event {
	if e.Name == "qrcode" {
		e.Data, _ = json.Marshal(struct {
			Event     string `json:"event"`
			Session   string `json:"session"`
			Available bool   `json:"available"`
		}{e.Name, e.Session, true})
	}
	return e
}
