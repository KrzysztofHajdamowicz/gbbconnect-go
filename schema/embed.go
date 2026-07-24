// Package schema embeds the published gbbconnect configuration schema.
package schema

import _ "embed"

// ConfigJSON is the Draft 2020-12 configuration schema.
//
//go:embed gbbconnect.schema.json
var ConfigJSON []byte
