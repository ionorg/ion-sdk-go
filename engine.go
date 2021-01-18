package engine

import (
	"sync"

	log "github.com/pion/ion-log"
)

type Engine struct {
	signal *Signal
	cfg    Config

	sync.RWMutex
	clients map[string]map[string]*Client
}

func NewEngine(addr string, cfg Config) *Engine {
	e := &Engine{
		signal:  NewSignal(addr),
		clients: make(map[string]map[string]*Client),
	}
	e.cfg = cfg

	fixByFile := []string{"asm_amd64.s", "proc.go", "icegatherer.go"}
	fixByFunc := []string{}

	log.Init(cfg.Log.Level, fixByFile, fixByFunc)
	return e
}

func (e *Engine) AddClient(addr, sid, cid string) *Client {
	if sid == "" || cid == "" {
		log.Errorf("invalid id: sid=%v cid=%v", sid, cid)
		return nil
	}

	e.Lock()
	if e.clients[sid] == nil {
		e.clients[sid] = make(map[string]*Client)
	}
	c := NewClient(addr, e.cfg.WebRTC)
	e.clients[sid][cid] = c
	e.Unlock()
	if c == nil {
		log.Errorf("c==nil")
		return nil
	}
	c.Join(sid)
	return c
}

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
		c.Leave()
	}
	delete(e.clients[sid], cid)
	e.Unlock()
	return nil
}
