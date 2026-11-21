package templates

// The drive and search tables render four columns. The header, every
// EntryRows branch and the load-more sentinel must agree, so the sentinel
// reads the width from here instead of carrying its own copy.
const driveEntryColumns = "4"
