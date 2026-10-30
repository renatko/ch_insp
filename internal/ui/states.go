package ui

func loadingText(message string) string { return mutedStyle.Render("Loading · " + message) }
func emptyText(message string) string   { return mutedStyle.Render("Empty · " + message) }
func errorText(message string) string   { return errorStyle.Render("Error · " + message) }
