package catalog

// timeWindowParams is the closed list of the query parameters that bound the
// time range of what an operation returns, the from... and to... pairs of the
// twelve list operations that take them (#204): an operation has a time window
// when its deployment serves one of them (Operation.HasTimeWindow). The hints
// that recommend a shorter time window rely on it, so it is never decided from
// a value, an argument or a payload.
var timeWindowParams = map[string]bool{
	"fromStartTime": true, "toStartTime": true,
	"fromTimestamp": true, "toTimestamp": true,
	"fromUpdatedAt": true, "toUpdatedAt": true,
}

// timeParam names a date-time query parameter of one operation.
type timeParam struct{ operationID, param string }

// notTimeWindows is the closed list of the date-time query parameters that
// bound no window (#204): the startTime of legacy_observationsV1_get is a hint
// for finding one observation by its ID, and the version of datasetItems_list
// is a point in time, the state of the dataset at that timestamp, not a smaller
// page. A date-time parameter that is on neither list fails a test, so a new
// one is never classified by its name alone. Both lists leave the value check of
// a date-time parameter (RFC 3339, before any request) untouched.
var notTimeWindows = map[timeParam]bool{
	{"legacy_observationsV1_get", "startTime"}: true,
	{"datasetItems_list", "version"}:           true,
}
