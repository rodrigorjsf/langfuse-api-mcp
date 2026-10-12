package sanitize

// RemovedPlaces exposes removedPlaces to the tests of package sanitize_test.
func RemovedPlaces(s string) (string, []int) { return removedPlaces(s) }
