package tunnel

import (
	"context"
	"encoding/base64"
	"io"
	"net"
	"sync"
	"time"

	"zyrln/relay/core"
)

const localPollWait = 5 * time.Millisecond

var b64EncPool = sync.Pool{
	New: func() any {
		b := make([]byte, base64.StdEncoding.EncodedLen(TunnelChunkSize))
		return &b
	},
}

func encodeTXChunk(chunk []byte) string {
	need := base64.StdEncoding.EncodedLen(len(chunk))
	if need <= TunnelChunkSize*4/3+8 {
		if p := b64EncPool.Get(); p != nil {
			bufPtr := p.(*[]byte)
			buf := *bufPtr
			if cap(buf) >= need {
				base64.StdEncoding.Encode(buf[:need], chunk)
				out := string(buf[:need])
				b64EncPool.Put(bufPtr)
				return out
			}
			b64EncPool.Put(bufPtr)
		}
	}
	return base64.StdEncoding.EncodeToString(chunk)
}

// RunTunnelBridge copies bytes between local and a relay tunnel session until both sides finish.
// The first Apps Script batch includes open+tx(+rx) so connect does not cost a separate round trip.
func RunTunnelBridge(ctx context.Context, local io.ReadWriter, sess *TunnelSession, target string, opTimeout time.Duration) {
	if sess == nil || sess.client == nil {
		core.Log("error", "tunnel bridge %s: nil session", target)
		return
	}
	sess.client.beginBridge()
	defer sess.client.endBridge()

	if opTimeout <= 0 {
		opTimeout = sess.client.timeout
	}

	ctx, cancel := context.WithCancel(ctx)
	defer cancel()
	defer sess.Close(context.Background())

	buf := make([]byte, TunnelChunkSize)
	pending := make([]byte, 0, TunnelChunkSize*2)
	wait := tunnelMinReadWait
	localDone := false
	lastDataAt := time.Now()
	localConn, _ := local.(net.Conn)

	shouldFlushTX := func() bool {
		if len(pending) == 0 {
			return false
		}
		if localDone {
			return true
		}
		if len(pending) >= TunnelChunkSize*MaxTXPerBatch {
			return true
		}
		return time.Since(lastDataAt) >= txCoalesceMaxWait
	}

	absorbLocal := func() {
		if localDone || localConn == nil {
			return
		}
		for len(pending) < TunnelChunkSize*MaxTXPerBatch {
			_ = localConn.SetReadDeadline(time.Now().Add(time.Millisecond))
			n, err := localConn.Read(buf)
			_ = localConn.SetReadDeadline(time.Time{})
			if n > 0 {
				pending = append(pending, buf[:n]...)
				lastDataAt = time.Now()
			}
			if err != nil {
				if err != io.EOF && ctx.Err() == nil {
					if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
						core.Log("error", "tunnel local read %s: %v", target, err)
						localDone = true
						return
					}
				}
				if err != io.EOF {
					if ne, ok := err.(net.Error); ok && ne.Timeout() {
						return
					}
				}
				localDone = true
				return
			}
			if n == 0 {
				return
			}
		}
	}

	prependOpen := func(ops []TunnelRequest) []TunnelRequest {
		if sess.opened.Load() || len(ops) == 0 {
			return ops
		}
		sess.openSent.Store(true)
		return append([]TunnelRequest{{Op: TunnelOpOpen, ID: sess.id, Target: sess.target}}, ops...)
	}

	appendTXOps := func(ops []TunnelRequest) []TunnelRequest {
		for len(pending) >= TunnelChunkSize && len(ops) < MaxTXPerBatch {
			chunk := pending[:TunnelChunkSize]
			pending = pending[TunnelChunkSize:]
			ops = append(ops, TunnelRequest{
				Op:   TunnelOpTX,
				Data: encodeTXChunk(chunk),
			})
		}
		if len(ops) < MaxTXPerBatch && len(pending) > 0 && (localDone || len(ops) == 0) {
			chunk := append([]byte(nil), pending...)
			pending = pending[:0]
			ops = append(ops, TunnelRequest{
				Op:   TunnelOpTX,
				Data: encodeTXChunk(chunk),
			})
		}
		return ops
	}

	markOpened := func(ops []TunnelRequest) {
		if sess.opened.Load() {
			return
		}
		for _, op := range ops {
			if op.Op == TunnelOpOpen {
				sess.opened.Store(true)
				return
			}
		}
	}

	exchange := func(ops []TunnelRequest) error {
		if len(ops) == 0 {
			return nil
		}
		ops = prependOpen(ops)
		exCtx, exCancel := context.WithTimeout(ctx, opTimeout)
		resps, err := sess.Exchange(exCtx, ops)
		exCancel()
		if err != nil {
			return err
		}
		markOpened(ops)
		if ops[len(ops)-1].Op != TunnelOpRX {
			return nil
		}
		resp := resps[len(resps)-1]
		if resp.Data == "" {
			if wait < tunnelMaxReadWait {
				wait += tunnelMinReadWait
				if wait > tunnelMaxReadWait {
					wait = tunnelMaxReadWait
				}
			}
			return nil
		}
		wait = tunnelMinReadWait
		data, err := base64.StdEncoding.DecodeString(resp.Data)
		if err != nil {
			return err
		}
		if _, err := local.Write(data); err != nil {
			return err
		}
		return nil
	}

	flushTX := func() error {
		for {
			ops := appendTXOps(nil)
			if len(ops) == 0 {
				return nil
			}
			moreTX := len(pending) >= TunnelChunkSize && !localDone
			ops = append(ops, TunnelRequest{
				Op:     TunnelOpRX,
				WaitMS: tunnelRXWaitMS(true, wait),
			})
			if err := exchange(ops); err != nil {
				return err
			}
			if !moreTX {
				return nil
			}
		}
	}

	flushRX := func() error {
		return exchange([]TunnelRequest{{
			Op:     TunnelOpRX,
			WaitMS: tunnelRXWaitMS(false, wait),
		}})
	}

	for {
		select {
		case <-ctx.Done():
			return
		default:
		}

		if !localDone {
			if localConn != nil {
				_ = localConn.SetReadDeadline(time.Now().Add(localPollWait))
				n, err := localConn.Read(buf)
				_ = localConn.SetReadDeadline(time.Time{})
				if n > 0 {
					pending = append(pending, buf[:n]...)
					lastDataAt = time.Now()
				}
				if err != nil {
					if err != io.EOF && ctx.Err() == nil {
						if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
							core.Log("error", "tunnel local read %s: %v", target, err)
							return
						}
					}
					if err == io.EOF {
						localDone = true
					} else if ne, ok := err.(net.Error); !ok || !ne.Timeout() {
						localDone = true
					}
				}
				absorbLocal()
			} else {
				n, err := local.Read(buf)
				if n > 0 {
					pending = append(pending, buf[:n]...)
					lastDataAt = time.Now()
				}
				if err != nil {
					localDone = true
					if err != io.EOF && ctx.Err() == nil {
						core.Log("error", "tunnel local read %s: %v", target, err)
						return
					}
				}
			}
		}

		if len(pending) > 0 {
			absorbLocal()
			if !shouldFlushTX() {
				continue
			}
			if err := flushTX(); err != nil {
				if ctx.Err() == nil {
					core.Log("error", "tunnel flush %s: %v", target, err)
				}
				return
			}
			continue
		}

		if !localDone {
			if err := flushRX(); err != nil {
				if ctx.Err() == nil {
					core.Log("error", "tunnel poll %s: %v", target, err)
				}
				return
			}
		}

		if localDone && len(pending) == 0 {
			_ = flushRX()
			return
		}
	}
}
