package watcher

// fxNoise are fx DI container lifecycle messages that add no value in watcher output.
var fxNoise = map[string]bool{
	"provided":                        true,
	"decorated":                       true,
	"supplied":                        true,
	"running":                         true,
	"started":                         true,
	"stopping":                        true,
	"stopped":                         true,
	"initialized custom fxevent.Logger": true,
}
