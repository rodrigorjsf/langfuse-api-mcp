package sanitize

// RemovedPlaces exposes removedPlaces to the tests of package sanitize_test.
func RemovedPlaces(s string) (string, []int) { return removedPlaces(s, removed) }

// LinePlaces exposes linePlaces to the tests of package sanitize_test.
func LinePlaces(s string) (string, []int) { return linePlaces(s) }
