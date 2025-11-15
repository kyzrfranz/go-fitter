package args

import (
	"fmt"
	"os"
)

func EnvOrDefault[T any](key string, defaultValue T) T {
	value, exists := os.LookupEnv(key)
	if !exists {
		return defaultValue
	}
	var v T
	switch any(v).(type) {
	case string:
		return any(value).(T)
	case int:
		//nolint:errcheck
		var intValue int
		_, _ = fmt.Sscanf(value, "%d", &intValue)
		return any(intValue).(T)
	case bool:
		//nolint:errcheck
		var boolValue bool
		_, _ = fmt.Sscanf(value, "%t", &boolValue)
		return any(boolValue).(T)
	default:
		return defaultValue
	}
}
