package speaker

import (
	crand "crypto/rand"
	"encoding/binary"

	exprand "golang.org/x/exp/rand"
)

// go-librespot draws its device ID (and other IDs) from golang.org/x/exp/rand,
// whose global source starts from the same seed in every process unless
// seeded. Unseeded, every sptui on every computer made the same device ID,
// so Spotify took them all for one device. Seed it before anything draws
// from it.
func init() {
	var b [8]byte
	_, _ = crand.Read(b[:])
	exprand.Seed(binary.LittleEndian.Uint64(b[:]))
}

// sharedDeviceID is the device ID the unseeded source makes. A saved state
// with it is given a new one.
const sharedDeviceID = "67d105493936e93431c7e42ff60e7c81405a4fe2"
