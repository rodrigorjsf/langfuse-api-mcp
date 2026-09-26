package catalog

// folderParam names one path parameter of one operation.
type folderParam struct{ operationID, param string }

// folderCapable is the allow-list of path parameters that take a Folder name:
// a prompt or dataset name whose "/"-separated segments place it inside
// folders. Only these accept "/", always sent percent-encoded as %2F so the
// name stays one path segment. The list fails closed: every other path
// parameter — IDs, datasets_getRun's runName, write operations — refuses "/".
// Write operations get their entries when execute_write is built (M4).
var folderCapable = map[folderParam]bool{
	{"prompts_get", "promptName"}:       true,
	{"datasets_get", "datasetName"}:     true,
	{"datasets_getRuns", "datasetName"}: true,
	{"datasets_getRun", "datasetName"}:  true,
}

// takesFolderName reports whether p is a path parameter of o that accepts a
// Folder name.
func (o Operation) takesFolderName(p Param) bool {
	return p.In == "path" && folderCapable[folderParam{o.ID, p.Name}]
}
