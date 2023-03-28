package signaling

import (
	"fmt"
	"sync"
	"time"

	grpc "github.com/thinkonmay/signaling-server/protocol/gRPC"
	ws "github.com/thinkonmay/signaling-server/protocol/websocket"

	"github.com/thinkonmay/signaling-server/protocol"
	"github.com/thinkonmay/signaling-server/validator"
)

type Signalling struct {
	waitLine map[string]protocol.Tenant
	mut      *sync.Mutex

	handlers  []protocol.ProtocolHandler
	validator validator.Validator
}

func InitSignallingServer(conf *protocol.SignalingConfig, provider validator.Validator) *Signalling {
	signaling := Signalling{
		waitLine:  make(map[string]protocol.Tenant),
		mut:       &sync.Mutex{},
		validator: provider,
		handlers: []protocol.ProtocolHandler{
			grpc.InitSignallingServer(conf),
			ws.InitSignallingWs(conf),
		},
	}

	go func() { // remove exited tenant from waiting like
		for {
			var rev []string
			signaling.mut.Lock()
			for index, wait := range signaling.waitLine {
				if wait.IsExited() {
					fmt.Printf("tenant exited\n")
					rev = append(rev, index)
				}
			}
			signaling.mut.Unlock()
			for _, i := range rev {
				signaling.removeTenant(i)
			}
			time.Sleep(100 * time.Millisecond)
		}
	}()
	go func() { // discard message from waiting like
		for {
			time.Sleep(100 * time.Millisecond)
			for _, t := range signaling.waitLine {
				if t.Peek() {
					_ = t.Receive() // discard
				}
			}
		}
	}()

	for _, handler := range signaling.handlers { // handle new tenant
		handler.OnTenant(func(token string, tent protocol.Tenant) error {
			signaling.addTenant(token, tent) // add tenant to queue

			// get all keys from current waiting line
			keys := make([]string, 0, len(signaling.waitLine))
			for k := range signaling.waitLine {
				keys = append(keys, k)
			}

			// validate every tenant in queue
			pairs, new_queue := signaling.validator.Validate(keys)

			// move tenant from waiting line to pair queue
			for _, v := range pairs {
				pair := Pair{A: nil, B: nil}
				for _, v2 := range keys {
					if v2 == v.PeerA && pair.B == nil {
						pair.B = signaling.waitLine[v2]
					} else if v2 == v.PeerB && pair.A == nil {
						pair.A = signaling.waitLine[v2]
					}
				}

				if pair.A == nil || pair.B == nil {
					continue
				}

				pair.handlePair()
			}

			// remove tenant in old queue if not exist in new queue
			for _, k := range keys {
				rm := true
				for _, n := range new_queue {
					if n == k {
						rm = false
					}
				}

				if rm {
					signaling.removeTenant(k)
				}
			}

			return nil
		})
	}

	return &signaling
}
