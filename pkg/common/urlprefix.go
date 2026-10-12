package common

// XPAM: public URL prefixes served by this component. nginx exposes them under the same paths
// and rewrites the legacy /koko/ and /koko/lion/ prefixes onto them.
const (
	URLPrefix        = "/secureconnection"
	DesktopURLPrefix = URLPrefix + "/desktop"
)
