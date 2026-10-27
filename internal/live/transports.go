package live

func (s Spec) singTransport() map[string]any {
	switch s.Transport {
	case "httpupgrade":
		m := map[string]any{"type": "httpupgrade", "path": s.Path}
		if s.WSHost != "" {
			m["host"] = s.WSHost
		}
		return m
	case "http":
		m := map[string]any{"type": "http", "path": s.Path}
		if s.WSHost != "" {
			m["host"] = []string{s.WSHost}
		}
		return m
	}
	return nil
}
