package aggregators

//TODO move some place else

func GetFloat(m map[string]any, key string) float64 {
	if m == nil {
		return 0
	}
	if val, ok := m[key]; ok {
		switch v := val.(type) {
		case float64:
			return v
		case float32:
			return float64(v)
		case int:
			return float64(v)
		case int32:
			return float64(v)
		case int64:
			return float64(v)
		}
	}
	return 0
}

func GetString(m map[string]any, key string) string {
	if m == nil {
		return "Unknown"
	}
	if val, ok := m[key]; ok {
		if s, ok := val.(string); ok {
			return s
		}
	}
	return "Unknown"
}
