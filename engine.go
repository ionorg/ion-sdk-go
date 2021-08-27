package engine

import (
	"fmt"
	"net/http"
	"sync"
	"time"

	_ "net/http/pprof"

	"github.com/lucsky/cuid"
	ilog "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
	"github.com/sirupsen/logrus"
)

var (
	log           *logrus.Logger
	DefaultConfig = Config{
		LogLevel: "info",
		WebRTC: WebRTCTransportConfig{
			Configuration: webrtc.Configuration{
				ICEServers: []webrtc.ICEServer{
					webrtc.ICEServer{
						URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
					},
				},
			},
		},
	}
)

func init() {
	ilog.Init(DefaultConfig.LogLevel)
	log = ilog.NewLoggerWithFields(ilog.StringToLevel(DefaultConfig.LogLevel), "engine", nil)
}

// Engine a sdk engine
type Engine struct {
	sync.RWMutex
	clients map[string]map[string]*Client
}

// NewEngine create a engine
func NewEngine() *Engine {
	e := &Engine{
		clients: make(map[string]map[string]*Client),
	}
	return e
}

// NewClient create a sdk client
func (e *Engine) NewClient(addr string, cid ...string) (*Client, error) {
	var uid string
	if len(cid) > 0 {
		uid = cid[0]
	}

	if uid == "" {
		uid = cuid.New()
	}

	s, err := NewSignal(addr, uid)
	if err != nil {
		return nil, err
	}

	c := &Client{
		engine:         e,
		uid:            uid,
		signal:         s,
		notify:         make(chan struct{}),
		remoteStreamId: make(map[string]string),
	}

	c.signal.OnNegotiate = c.negotiate
	c.signal.OnTrickle = c.trickle
	c.signal.OnSetRemoteSDP = c.setRemoteSDP
	c.signal.OnTrackEvent = c.trackEvent
	c.signal.OnSpeaker = c.speaker
	c.signal.OnError = func(err error) {
		if c.OnError != nil {
			c.OnError(err)
		}
	}

	c.pub = NewTransport(Target_PUBLISHER, c.signal)
	c.sub = NewTransport(Target_PUBLISHER, c.signal)

	e.addClient(c)
	return c, nil
}

// addClient add a client
// addr: grpc addr
// sid: session/room id
// cid: client id
func (e *Engine) addClient(c *Client) {
	e.Lock()
	defer e.Unlock()
	if e.clients[c.sid] == nil {
		e.clients[c.sid] = make(map[string]*Client)
	}

	e.clients[c.sid][c.uid] = c
}

// delClient delete a client
func (e *Engine) delClient(c *Client) {
	e.Lock()
	if e.clients[c.sid] == nil {
		e.Unlock()
		log.Errorf("error: %v", errInvalidSessID)
	}
	if c, ok := e.clients[c.sid][c.uid]; ok && (c != nil) {
		c.Close()
	}
	delete(e.clients[c.sid], c.uid)
	e.Unlock()
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
