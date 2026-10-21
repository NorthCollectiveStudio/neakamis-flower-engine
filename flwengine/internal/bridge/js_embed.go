//go:build !flwprodslim

package bridge

import _ "embed"

// The bridge is the part of the engine that runs inside the game page. In
// a development build it is part of the binary, in a production build it is
// only in the signed and encrypted runtime package (see js_slim.go), so
// that it cannot be read out of the engine with strings.
//
//go:embed web/flwbridge.js
var bridgeJS string

func (b *Bridge) JS() string {
        return bridgeJS
}

func JS() string {
        return bridgeJS
}
