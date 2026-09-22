package netx

// NetworkProbeResult reports the test status and latency of an upstream target.
type NetworkProbeResult struct {
	Available bool   `json:"available"`
	LatencyMS int64  `json:"latency_ms,omitempty"`
	Error     string `json:"error,omitempty"`
}

// NetworkTestResponse contains probe results for both JavDB and JavBus.
type NetworkTestResponse struct {
	JavDB  NetworkProbeResult `json:"javdb"`
	JavBus NetworkProbeResult `json:"javbus"`
}
