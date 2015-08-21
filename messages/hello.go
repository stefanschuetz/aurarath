package messages
import (
	"github.com/joernweissenborn/aurarath/network/peer"
	"strings"
	"encoding/gob"
	"bytes"
)


type Hello struct {
	PeerDetails peer.Details
	Address string
	Port int
}

func(*Hello) GetType() MessageType {return HELLO}


func(h *Hello) Unflatten(d []string)  {
	dec := gob.NewDecoder(strings.NewReader(d[0]))
	dec.Decode(&h.PeerDetails)
}

func(h *Hello) Flatten() [][]byte {
	var payload bytes.Buffer
	enc := gob.NewEncoder(&payload)
	enc.Encode(h.PeerDetails)
	return [][]byte{payload.Bytes()}
}
