package configuration

// houseSets is a zero-config configuration's per-file dispatch.
type houseSets struct {
	detection HouseDetection
	variants  map[houseVariant]*Configuration
	applied   []AppliedSet
	// gatedReasons is why each rule a per-file set turns on is off where that set does not apply.
	gatedReasons map[string]OffReason
}

// variantFor is the configuration a file resolves through under zero config.
func (house *houseSets) variantFor(path string) *Configuration {
	return house.variants[houseVariant{react: house.detection.ReactFiles[path], next: house.detection.NextFiles[path]}]
}
