//go:build flwprodslim

package bridge

// In a production build the bridge is NOT part of the engine binary.
//
// The runtime package (encrypted and signed) holds the bridge, and the
// game host serves it from there. If the bridge were embedded as well, the
// whole source of the engine would be readable in the binary of the
// production build - the encryption of the runtime package would be
// pointless.

func (b *Bridge) JS() string {
        return ""
}

func JS() string {
        return ""
}
