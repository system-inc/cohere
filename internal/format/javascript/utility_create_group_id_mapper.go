package javascript

import "github.com/system-inc/cohere/internal/format/doc"

// utilities/create-group-id-mapper.js.

// createGroupIdMapper is upstream's createGroupIdMapper: one group id per node, minted on first ask.
// Upstream keeps a module-level WeakMap per mapper; Go has no weak map, so the ids live on the
// format's settings, keyed on the description and then the node, and the returned function takes
// options to reach them.
func createGroupIdMapper(description string) func(options *Options, node Node) *doc.GroupId {
	return func(options *Options, node Node) *doc.GroupId {
		formatSettings := settingsOf(options)
		if formatSettings.groupIds == nil {
			formatSettings.groupIds = map[string]map[Node]*doc.GroupId{}
		}
		groupIds := formatSettings.groupIds[description]
		if groupIds == nil {
			groupIds = map[Node]*doc.GroupId{}
			formatSettings.groupIds[description] = groupIds
		}
		if groupId, present := groupIds[node]; present {
			return groupId
		}
		groupId := newGroupId(description)
		groupIds[node] = groupId
		return groupId
	}
}
