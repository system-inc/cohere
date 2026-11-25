package load

// slotWork is the kind of work a load's slot is granted for, which the test hook is told (see loader.observeGrant).
type slotWork uint8

const (
	slotWorkRead slotWork = iota
	slotWorkParse
	slotWorkCheck
	slotWorkExportData
)
