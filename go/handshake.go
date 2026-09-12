package pluginsdk

import "github.com/hashicorp/go-plugin"

// Handshake is the go-plugin handshake shared by the host and every plugin.
// The host must use exactly this config when creating plugin clients.
var Handshake = plugin.HandshakeConfig{
	ProtocolVersion:  1,
	MagicCookieKey:   "SONICORE_PLUGIN",
	MagicCookieValue: "sonicore_plugin_v1",
}
