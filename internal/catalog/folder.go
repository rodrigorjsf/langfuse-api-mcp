package catalog

// folderParam names one path parameter of one operation.
type folderParam struct{ operationID, param string }

// folderCapable is the allow-list of path parameters that take a Folder name:
// a prompt or dataset name whose "/"-separated segments place it inside
// folders. Only these accept "/", always sent percent-encoded as %2F so the
// name stays one path segment. The list fails closed: every other path
// parameter (IDs, a dataset run's runName) refuses "/". The write entries
// (#113) were checked against the Langfuse API reference:
// promptVersion_update's prompt name "must be URL encoded" when it is in a
// folder (it was promptName, on .../version/{version}, before 3.18.0);
// prompts_delete takes the same prompt name on the prompts_get route; a
// dataset name with slashes is URL encoded as any path parameter.
var folderCapable = map[folderParam]bool{
	{"prompts_get", "promptName"}:          true,
	{"datasets_get", "datasetName"}:        true,
	{"datasets_getRuns", "datasetName"}:    true,
	{"datasets_getRun", "datasetName"}:     true,
	{"prompts_delete", "promptName"}:       true,
	{"promptVersion_update", "name"}:       true,
	{"promptVersion_update", "promptName"}: true,
	{"datasets_deleteRun", "datasetName"}:  true,
}

// takesFolderName reports whether p is a path parameter of o that accepts a
// Folder name.
func (o Operation) takesFolderName(p Param) bool {
	return p.In == "path" && folderCapable[folderParam{o.ID, p.Name}]
}
