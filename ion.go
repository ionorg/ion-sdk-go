package engine

import (
	"github.com/pion/webrtc/v3"
)

type PeerState int32

const (
	PeerJOIN   PeerState = 0
	PeerUPDATE PeerState = 1
	PeerLEAVE  PeerState = 2
)

type Peer struct {
	Sid  string
	Uid  string
	Info map[string]interface{}
}

type PeerEvent struct {
	State PeerState
	Peer  Peer
}

type Track struct {
	Id        string
	Label     string
	Kind      string
	Simulcast map[string]string
}

type Stream struct {
	Id     string
	Tracks []*Track
}

type StreamState int32

const (
	StreamADD    StreamState = 0
	StreamREMOVE StreamState = 2
)

type StreamEvent struct {
	State   StreamState
	Sid     string
	Uid     string
	Streams []*Stream
}

type Message struct {
	From string
	To   string
	Data map[string]interface{}
}

type IonConnector struct {
	url      string
	engine   *Engine
	biz      *BizClient
	sfu      *Client
	uid, sid string
	pinfo    map[string]interface{}

	OnJoin        func(success bool, reason string)
	OnLeave       func(reason string)
	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(dc *webrtc.DataChannel)
	OnPeerEvent   func(event PeerEvent)
	OnStreamEvent func(event StreamEvent)
	OnMessage     func(msg Message)
	OnError       func(error)
}

func NewIonConnector(addr string, uid string, pinfo map[string]interface{}) *IonConnector {
	i := &IonConnector{
		uid:   uid,
		url:   addr,
		pinfo: pinfo,
		biz:   NewBizClient(addr),
	}
	// add stun servers
	webrtcCfg := webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{
				URLs: []string{"stun:stun.stunprotocol.org:3478", "stun:stun.l.google.com:19302"},
			},
		},
	}
	config := Config{
		WebRTC: WebRTCTransportConfig{
			Configuration: webrtcCfg,
		},
	}

	// new sdk engine
	i.engine = NewEngine(config)

	i.biz.OnJoin = func(success bool, reason string) {
		if success {
			// create a new client from engine
			c, err := NewClient(i.engine, i.url, i.uid)
			if err != nil {
				log.Errorf("err=%v", err)
				return
			}

			c.OnTrack = func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
				if i.OnTrack != nil {
					i.OnTrack(track, receiver)
				}
			}

			c.OnDataChannel = func(dc *webrtc.DataChannel) {
				if i.OnDataChannel != nil {
					i.OnDataChannel(dc)
				}
			}

			c.OnError = func(err error) {
				if i.OnError != nil {
					i.OnError(err)
				}
			}

			c.Join(i.sid, nil)

			i.sfu = c
		}

		if i.OnJoin != nil {
			i.OnJoin(success, reason)
		}
	}
	i.biz.OnLeave = func(reason string) {
		if i.sfu != nil {
			i.sfu.Close()
			i.sfu = nil
		}
		if i.OnLeave != nil {
			i.OnLeave(reason)
		}
	}
	i.biz.OnError = func(e error) {
		if i.OnError != nil {
			i.OnError(e)
		}
	}
	i.biz.OnPeerEvent = func(state PeerState, peer Peer) {
		if i.OnPeerEvent != nil {
			i.OnPeerEvent(PeerEvent{
				State: state,
				Peer:  peer,
			})
		}
	}
	i.biz.OnStreamEvent = func(state StreamState, sid, uid string, streams []*Stream) {
		if i.OnStreamEvent != nil {
			i.OnStreamEvent(StreamEvent{
				State:   state,
				Sid:     sid,
				Uid:     uid,
				Streams: streams,
			})
		}
	}
	i.biz.OnMessage = func(from, to string, data map[string]interface{}) {
		if i.OnMessage != nil {
			i.OnMessage(Message{
				From: from,
				To:   to,
				Data: data,
			})
		}
	}
	return i
}

func (i *IonConnector) SFU() *Client {
	return i.sfu
}

func (i *IonConnector) Join(sid string) error {
	i.sid = sid
	return i.biz.Join(i.sid, i.uid, i.pinfo)
}

func (i *IonConnector) Leave(uid string) error {
	return i.biz.Leave(uid)
}

func (i *IonConnector) Message(from string, to string, data map[string]interface{}) {
	i.biz.SendMessage(from, to, data)
}

func (i *IonConnector) Close() {
	i.biz.Close()
	i.sfu.Close()
}
