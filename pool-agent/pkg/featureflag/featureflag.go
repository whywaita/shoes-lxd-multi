package featureflag

import (
	"os"
	"strings"
)

// FeatureFlag represents available feature flags
type FeatureFlag string

const (
	// CountWithoutRunning disables counting Running instances in pool count
	CountWithoutRunning FeatureFlag = "count-without-running"
)

// featureFlags holds the current state of feature flags
var featureFlags map[FeatureFlag]bool

func init() {
	LoadFromEnv()
}

// LoadFromEnv loads feature flags from FEATUREFLAG environment variable
func LoadFromEnv() {
	featureFlags = make(map[FeatureFlag]bool)

	flagsEnv := os.Getenv("FEATUREFLAG")
	if flagsEnv == "" {
		return
	}

	flags := strings.Split(flagsEnv, ",")
	for _, flag := range flags {
		flag = strings.TrimSpace(flag)
		if flag != "" {
			featureFlags[FeatureFlag(flag)] = true
		}
	}
}

// IsEnabled checks if a feature flag is enabled
func IsEnabled(flag FeatureFlag) bool {
	return featureFlags[flag]
}
