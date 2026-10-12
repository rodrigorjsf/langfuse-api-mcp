package sanitize

// RemovedPlaces exposes removedPlaces to the tests of package sanitize_test.
func RemovedPlaces(s string) (string, []int) {
	text, places, _ := removedPlaces(s, removed)
	return text, places
}

// LinePlaces exposes linePlaces to the tests of package sanitize_test.
func LinePlaces(s string) (string, []int) {
	text, places, _ := linePlaces(s)
	return text, places
}
