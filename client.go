package engine

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"time"

	"github.com/lucsky/cuid"
	"github.com/pion/webrtc/v3"
)

func NewJoinConfig() *JoinConfig {
	m := make(JoinConfig)
	return &m
}

type JoinConfig map[string]string

func (j JoinConfig) SetNoPublish() *JoinConfig {
	j["NoPublish"] = "true"
	return &j
}

func (j JoinConfig) SetNoSubscribe() *JoinConfig {
	j["NoSubscribe"] = "true"
	return &j
}

func SetRelay(j JoinConfig) *JoinConfig {
	j["Relay"] = "true"
	return &j
}

const (
	API_CHANNEL = "ion-sfu"
	PUBLISHER   = 0
	SUBSCRIBER  = 1
)

//Call dc api
type Call struct {
	StreamID string `json:"streamId"`
	Video    string `json:"video"`
	Audio    bool   `json:"audio"`
}

// Client a sdk client
type Client struct {
	uid    string
	sid    string
	pub    *Transport
	sub    *Transport
	cfg    WebRTCTransportConfig
	signal *Signal

	//export to user
	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(*webrtc.DataChannel)
	OnError       func(error)

	producer *WebMProducer
	recvByte int
	notify   chan struct{}

	//cache remote sid for subscribe/unsubscribe
	streamLock     sync.RWMutex
	remoteStreamId map[string]string

	//cache datachannel api operation before dc.OnOpen
	apiQueue []Call

	engine *Engine
}

// NewClient create a sdk client
func NewClient(engine *Engine, addr string, cid string) (*Client, error) {
	uid := cid
	if uid == "" {
		uid = cuid.New()
	}

	s, err := NewSignal(addr, uid)
	if err != nil {
		return nil, err
	}
	c := &Client{
		engine:         engine,
		uid:            uid,
		signal:         s,
		cfg:            engine.cfg.WebRTC,
		notify:         make(chan struct{}),
		remoteStreamId: make(map[string]string),
	}

	c.signal.OnNegotiate = c.Negotiate
	c.signal.OnTrickle = c.Trickle
	c.signal.OnSetRemoteSDP = c.SetRemoteSDP
	c.signal.OnError = func(err error) {
		if c.OnError != nil {
			c.OnError(err)
		}
	}

	c.pub = NewTransport(PUBLISHER, c.signal, c.cfg)
	c.sub = NewTransport(SUBSCRIBER, c.signal, c.cfg)

	// this will be called when pub add/remove/replace track, but pion never triger, why?
	// c.pub.pc.OnNegotiationNeeded(c.OnNegotiationNeeded)
	return c, nil
}

// SetRemoteSDP pub SetRemoteDescription and send cadidate to sfu
func (c *Client) SetRemoteSDP(sdp webrtc.SessionDescription) error {
	err := c.pub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
		return err
	}

	// it's safe to add cand now after SetRemoteDescription
	if len(c.pub.RecvCandidates) > 0 {
		for _, candidate := range c.pub.RecvCandidates {
			log.Debugf("id=%v c.pub.pc.AddICECandidate candidate=%v", c.uid, candidate)
			err = c.pub.pc.AddICECandidate(candidate)
			if err != nil {
				log.Errorf("id=%v c.pub.pc.AddICECandidate err=%v", c.uid, err)
			}
		}
		c.pub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// it's safe to send cand now after join ok
	if len(c.pub.SendCandidates) > 0 {
		for _, cand := range c.pub.SendCandidates {
			log.Debugf("id=%v sending c.pub.SendCandidates cand=%v", c.uid, cand)
			c.signal.Trickle(cand, PUBLISHER)
		}
		c.pub.SendCandidates = []*webrtc.ICECandidate{}
	}
	return nil
}

// Join client join a session
func (c *Client) Join(sid string, config *JoinConfig) error {
	log.Debugf("[Client.Join] sid=%v uid=%v", sid, c.uid)
	c.sub.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Debugf("[c.sub.pc.OnTrack] got track streamId=%v kind=%v ssrc=%v ", track.StreamID(), track.Kind(), track.SSRC())
		c.streamLock.Lock()
		c.remoteStreamId[track.StreamID()] = track.StreamID()
		log.Debugf("id=%v len(c.remoteStreamId)=%+v", c.uid, len(c.remoteStreamId))
		c.streamLock.Unlock()
		// user define
		if c.OnTrack != nil {
			c.OnTrack(track, receiver)
		} else {
			//for read and calc
			b := make([]byte, 1500)
			for {
				select {
				case <-c.notify:
					return
				default:
					n, _, err := track.Read(b)
					if err != nil {
						if err == io.EOF {
							log.Errorf("id=%v track.ReadRTP err=%v", c.uid, err)
							return
						}
						log.Errorf("id=%v Error reading track rtp %s", c.uid, err)
						continue
					}
					c.recvByte += n
				}
			}
		}
	})

	c.sub.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Debugf("id=%v [c.sub.pc.OnDataChannel] got dc %v", c.uid, dc.Label())
		if dc.Label() == API_CHANNEL {
			log.Debugf("%v got dc %v", c.uid, dc.Label())
			c.sub.api = dc
			// send cmd after open
			c.sub.api.OnOpen(func() {
				if len(c.apiQueue) > 0 {
					for _, cmd := range c.apiQueue {
						log.Debugf("%v c.sub.api.OnOpen send cmd=%v", c.uid, cmd)
						marshalled, err := json.Marshal(cmd)
						if err != nil {
							continue
						}
						err = c.sub.api.Send(marshalled)
						if err != nil {
							log.Errorf("id=%v err=%v", c.uid, err)
						}
						time.Sleep(time.Millisecond * 10)
					}
					c.apiQueue = []Call{}
				}
			})
			return
		}
		log.Debugf("%v got dc %v", c.uid, dc.Label())
		if c.OnDataChannel != nil {
			c.OnDataChannel(dc)
		}
	})

	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		return err
	}
	err = c.pub.pc.SetLocalDescription(offer)
	if err != nil {
		return err
	}
	err = c.signal.Join(sid, c.uid, offer, config)
	if err == nil {
		c.sid = sid
		c.engine.AddClient(c)
	}
	return err
}

// GetPubStats get pub stats
func (c *Client) GetPubStats() webrtc.StatsReport {
	return c.pub.pc.GetStats()
}

// GetSubStats get sub stats
func (c *Client) GetSubStats() webrtc.StatsReport {
	return c.sub.pc.GetStats()
}

func (c *Client) GetPubTransport() *Transport {
	return c.pub
}

func (c *Client) GetSubTransport() *Transport {
	return c.sub
}

// Publish a local track
func (c *Client) Publish(track webrtc.TrackLocal) (*webrtc.RTPTransceiver, error) {
	t, err := c.pub.pc.AddTransceiverFromTrack(track, webrtc.RTPTransceiverInit{
		Direction: webrtc.RTPTransceiverDirectionSendonly,
	})
	c.OnNegotiationNeeded()
	return t, err
}

// UnPublish a local track by Transceiver
func (c *Client) UnPublish(t *webrtc.RTPTransceiver) error {
	err := c.pub.pc.RemoveTrack(t.Sender())
	c.OnNegotiationNeeded()
	return err
}

// Close client close
func (c *Client) Close() {
	log.Debugf("id=%v", c.uid)
	close(c.notify)
	if c.pub != nil {
		c.pub.pc.Close()
	}
	if c.sub != nil {
		c.sub.pc.Close()
	}
	if c.producer != nil {
		c.producer.Stop()
	}
	c.signal.Close()
	c.engine.RemoveClient(c)
}

// CreateDataChannel create a custom datachannel
func (c *Client) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	log.Debugf("id=%v CreateDataChannel %v", c.uid, label)
	return c.pub.pc.CreateDataChannel(label, &webrtc.DataChannelInit{})
}

// Trickle receive candidate from sfu and add to pc
func (c *Client) Trickle(candidate webrtc.ICECandidateInit, target int) {
	log.Debugf("id=%v candidate=%v target=%v", c.uid, candidate, target)
	var t *Transport
	if target == SUBSCRIBER {
		t = c.sub
	} else {
		t = c.pub
	}

	if t.pc.CurrentRemoteDescription() == nil {
		t.RecvCandidates = append(t.RecvCandidates, candidate)
	} else {
		err := t.pc.AddICECandidate(candidate)
		if err != nil {
			log.Errorf("id=%v err=%v", c.uid, err)
		}
	}

}

// Negotiate sub negotiate
func (c *Client) Negotiate(sdp webrtc.SessionDescription) error {
	log.Debugf("id=%v Negotiate sdp=%v", c.uid, sdp)
	// 1.sub set remote sdp
	err := c.sub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("id=%v Negotiate c.sub.pc.SetRemoteDescription err=%v", c.uid, err)
		return err
	}

	// 2. safe to send candiate to sfu after join ok
	if len(c.sub.SendCandidates) > 0 {
		for _, cand := range c.sub.SendCandidates {
			log.Debugf("id=%v send sub.SendCandidates c.uid, c.signal.Trickle cand=%v", c.uid, cand)
			c.signal.Trickle(cand, SUBSCRIBER)
		}
		c.sub.SendCandidates = []*webrtc.ICECandidate{}
	}

	// 3. safe to add candidate after SetRemoteDescription
	if len(c.sub.RecvCandidates) > 0 {
		for _, candidate := range c.sub.RecvCandidates {
			log.Debugf("id=%v Negotiate c.sub.pc.AddICECandidate candidate=%v", c.uid, candidate)
			_ = c.sub.pc.AddICECandidate(candidate)
		}
		c.sub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// 4. create answer after add ice candidate
	answer, err := c.sub.pc.CreateAnswer(nil)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
		return err
	}

	// 5. set local sdp(answer)
	err = c.sub.pc.SetLocalDescription(answer)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
		return err
	}

	// 6. send answer to sfu
	c.signal.Answer(answer)

	return err
}

// OnNegotiationNeeded will be called when add/remove track, but never trigger, call by hand
func (c *Client) OnNegotiationNeeded() {
	// 1. pub create offer
	offer, err := c.pub.pc.CreateOffer(nil)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
	}

	// 2. pub set local sdp(offer)
	err = c.pub.pc.SetLocalDescription(offer)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
	}

	log.Debugf("id=%v OnNegotiationNeeded!! c.pub.pc.CreateOffer and send offer=%v", c.uid, offer)
	//3. send offer to sfu
	c.signal.Offer(offer)
}

// selectRemote select remote video/audio
func (c *Client) selectRemote(streamId, video string, audio bool) error {
	log.Debugf("id=%v streamId=%v video=%v audio=%v", c.uid, streamId, video, audio)
	call := Call{
		StreamID: streamId,
		Video:    video,
		Audio:    audio,
	}

	// cache cmd when dc not ready
	if c.sub.api == nil || c.sub.api.ReadyState() != webrtc.DataChannelStateOpen {
		log.Debugf("id=%v append to c.apiQueue call=%v", c.uid, call)
		c.apiQueue = append(c.apiQueue, call)
		return nil
	}

	// send cached cmd
	if len(c.apiQueue) > 0 {
		for _, cmd := range c.apiQueue {
			log.Debugf("id=%v c.sub.api.Send cmd=%v", c.uid, cmd)
			marshalled, err := json.Marshal(cmd)
			if err != nil {
				continue
			}
			err = c.sub.api.Send(marshalled)
			if err != nil {
				log.Errorf("err=%v", err)
			}
			time.Sleep(time.Millisecond * 10)
		}
		c.apiQueue = []Call{}
	}

	// send this cmd
	log.Debugf("id=%v c.sub.api.Send call=%v", c.uid, call)
	marshalled, err := json.Marshal(call)
	if err != nil {
		return err
	}
	err = c.sub.api.Send(marshalled)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
	}
	return err
}

// UnSubscribeAll unsubscribe all stream
func (c *Client) UnSubscribeAll() {
	c.streamLock.RLock()
	m := c.remoteStreamId
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Debugf("id=%v UnSubscribe remote streamid=%v", c.uid, streamId)
		c.selectRemote(streamId, "none", false)
	}
}

// SubscribeAll subscribe all stream with the same video/audio param
func (c *Client) SubscribeAll(video string, audio bool) {
	c.streamLock.RLock()
	m := c.remoteStreamId
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Debugf("id=%v Subscribe remote streamid=%v", c.uid, streamId)
		c.selectRemote(streamId, video, audio)
	}
}

// PublishWebm publish a webm producer
func (c *Client) PublishWebm(file string, video, audio bool) error {
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		c.producer = NewWebMProducer(c.uid, file, 0)
	default:
		return errInvalidFile
	}
	if video {
		_, err := c.producer.AddTrack(c.pub.pc, "video")
		if err != nil {
			log.Debugf("err=%v", err)
			return err
		}
	}
	if audio {
		_, err := c.producer.AddTrack(c.pub.pc, "audio")
		if err != nil {
			log.Debugf("err=%v", err)
			return err
		}
	}
	c.producer.Start()
	//trigger by hand
	c.OnNegotiationNeeded()
	return nil
}

func (c *Client) getBandWidth(cycle int) (int, int) {
	var recvBW, sendBW int
	if c.producer != nil {
		sendBW = c.producer.GetSendBandwidth(cycle)
	}

	recvBW = c.recvByte / cycle / 1000
	c.recvByte = 0
	return recvBW, sendBW
}

func (c *Client) Simulcast(layer string) {
	if layer == "" {
		return
	}
	c.streamLock.RLock()
	m := c.remoteStreamId
	log.Infof("Simulcast: streams=%v", m)
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Debugf("id=%v simulcast remote streamid=%v", c.uid, streamId)
		c.selectRemote(streamId, layer, true)
	}
}
