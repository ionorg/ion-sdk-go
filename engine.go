package engine

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	_ "net/http/pprof"

	ilog "github.com/pion/ion-log"
)

var (
	log = ilog.NewLoggerWithFields(ilog.DebugLevel, "engine", nil)
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

	return e
}

// AddClient add a client
// addr: grpc addr
// sid: session/room id
// cid: client id
func (e *Engine) AddClient(c *Client) error {
	e.Lock()
	defer e.Unlock()
	if e.clients[c.sid] == nil {
		e.clients[c.sid] = make(map[string]*Client)
	}

	e.clients[c.sid][c.uid] = c
	if c == nil {
		err := fmt.Errorf("client is nil")
		log.Errorf("%v", err)
		return err
	}

	return nil
}

// DelClient delete a client
func (e *Engine) DelClient(c *Client) error {
	e.Lock()
	if e.clients[c.sid] == nil {
		e.Unlock()
		return errInvalidSessID
	}
	if c, ok := e.clients[c.sid][c.uid]; ok && (c != nil) {
        delete(e.clients[c.sid], c.uid)
	}
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

// ServePProf listening pprof
func (e *Engine) ServePProf(paddr string) {
	log.Infof("PProf Listening %v", paddr)
	err := http.ListenAndServe(paddr, nil)
	if err != nil {
		log.Errorf("ServePProf error:%v", err)
	}
}
