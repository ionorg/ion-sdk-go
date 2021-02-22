package engine

import (
	"fmt"
	"sync"
	"time"

	log "github.com/pion/ion-log"
)

// Engine a sdk engine
type Engine struct {
	cfg Config

	sync.RWMutex
	clients map[string]map[string]*Client
}

// NewEngine create a engine
func NewEngine(cfg Config) *Engine {
	e := &Engine{
		clients: make(map[string]map[string]*Client),
	}
	e.cfg = cfg

	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go", "client.go", "signal.go"}
	fixByFunc := []string{}

	log.Init(cfg.Log.Level, fixByFile, fixByFunc)
	return e
}

// AddClient add a client
// addr: grpc addr
// sid: session/room id
// cid: client id
func (e *Engine) AddClient(addr, sid, cid string) *Client {
	if sid == "" || cid == "" {
		log.Errorf("invalid id: sid=%v cid=%v", sid, cid)
		return nil
	}

	e.Lock()
	defer e.Unlock()
	if e.clients[sid] == nil {
		e.clients[sid] = make(map[string]*Client)
	}
	c := NewClient(addr, cid, e.cfg.WebRTC)
	if c == nil {
		return nil
	}
	e.clients[sid][cid] = c
	if c == nil {
		log.Errorf("c==nil")
		return nil
	}
	return c
}

// DelClient delete a client
func (e *Engine) DelClient(sid, cid string) error {
	if cid == "" {
		return errInvalidClientID
	}

	e.Lock()
	if e.clients[sid] == nil {
		e.Unlock()
		return errInvalidSessID
	}
	if c, ok := e.clients[sid][cid]; ok && (c != nil) {
		c.Close()
	}
	delete(e.clients[sid], cid)
	e.Unlock()
	return nil
}

// Stats show a total stats to console: clients and bandwidth
func (e *Engine) Stats(cycle int) string {
	for {
		info := "\n-------stats-------\n"

		e.RLock()
		if len(e.clients) == 0 {
			e.RUnlock()
			continue
		}
		n := 0
		for _, m := range e.clients {
			n += len(m)
		}
		info += fmt.Sprintf("Clients: %d\n", n)

		totalRecvBW, totalSendBW := 0, 0
		for _, m := range e.clients {
			for _, c := range m {
				if c == nil {
					continue
				}
				recvBW, sendBW := c.getBandWidth(cycle)
				totalRecvBW += recvBW
				totalSendBW += sendBW
			}
		}

		info += fmt.Sprintf("RecvBandWidth: %d KB/s\n", totalRecvBW)
		info += fmt.Sprintf("SendBandWidth: %d KB/s\n", totalSendBW)
		e.RUnlock()
		log.Infof(info)
		time.Sleep(time.Duration(cycle) * time.Second)
	}
}
