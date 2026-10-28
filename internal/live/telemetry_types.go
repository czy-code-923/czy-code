package live

import "time"

type TrafficCounter struct {
	ID      string `json:"id"`
	Kind    string `json:"kind"`
	Subject string `json:"subject_id"`
	Up      int64  `json:"uplink"`
	Down    int64  `json:"downlink"`
}
type AccessObservation struct {
	ID          string    `json:"id"`
	User        string    `json:"user_id"`
	Service     string    `json:"service_id"`
	Source      string    `json:"source_ip"`
	Destination string    `json:"destination"`
	Port        int       `json:"destination_port"`
	Network     string    `json:"network"`
	ObservedAt  time.Time `json:"observed_at,omitempty"`
	QueuedAt    time.Time `json:"queued_at,omitempty"`
	Up          int64     `json:"uplink"`
	Down        int64     `json:"downlink"`
	BytesKnown  bool      `json:"bytes_known"`
	Closed      bool      `json:"closed"`
}
type MeterStatus struct {
	Engine string `json:"engine"`
	State  string `json:"state"`
	Error  string `json:"error"`
}
type TelemetryBatch struct {
	Counters []TrafficCounter    `json:"counters"`
	Access   []AccessObservation `json:"access"`
	Status   []MeterStatus       `json:"status"`
}
