package engine

import (
	"encoding/json"
	"io"
	"path/filepath"
	"sync"
	"time"

	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

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
	ID     string
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
}

// NewClient create a sdk client
func NewClient(addr, id string, cfg WebRTCTransportConfig) *Client {
	s := NewSignal(addr, id)
	if s == nil {
		return nil
	}
	c := &Client{
		ID:             id,
		signal:         s,
		cfg:            cfg,
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
	return c
}

// SetRemoteSDP pub SetRemoteDescription and send cadidate to sfu
func (c *Client) SetRemoteSDP(sdp webrtc.SessionDescription) error {
	err := c.pub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("id=%v err=%v", c.ID, err)
		return err
	}

	// it's safe to add cand now after SetRemoteDescription
	if len(c.pub.RecvCandidates) > 0 {
		for _, candidate := range c.pub.RecvCandidates {
			log.Debugf("id=%v c.pub.pc.AddICECandidate candidate=%v", c.ID, candidate)
			err = c.pub.pc.AddICECandidate(candidate)
			if err != nil {
				log.Errorf("id=%v c.pub.pc.AddICECandidate err=%v", c.ID, err)
			}
		}
		c.pub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// it's safe to send cand now after join ok
	if len(c.pub.SendCandidates) > 0 {
		for _, cand := range c.pub.SendCandidates {
			log.Debugf("id=%v sending c.pub.SendCandidates cand=%v", c.ID, cand)
			c.signal.Trickle(cand, PUBLISHER)
		}
		c.pub.SendCandidates = []*webrtc.ICECandidate{}
	}
	return nil
}

// Join client join a session
func (c *Client) Join(sid string) error {
	log.Debugf("[Client.Join] sid=%v id=%v", sid, c.ID)
	c.sub.pc.OnTrack(func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver) {
		log.Debugf("[c.sub.pc.OnTrack] got track streamId=%v kind=%v ssrc=%v ", track.StreamID(), track.Kind(), track.SSRC())
		c.streamLock.Lock()
		c.remoteStreamId[track.StreamID()] = track.StreamID()
		log.Debugf("id=%v len(c.remoteStreamId)=%+v", c.ID, len(c.remoteStreamId))
		c.streamLock.Unlock()
		// user define
		if c.OnTrack != nil {
			c.OnTrack(track, receiver)
		} else {
			//for read and calc
			for {
				select {
				case <-c.notify:
					return
				default:
					pkt, _, err := track.ReadRTP()
					if err != nil {
						if err == io.EOF {
							log.Errorf("id=%v track.ReadRTP err=%v", c.ID, err)
							return
						}
						log.Errorf("id=%v Error reading track rtp %s", c.ID, err)
						continue
					}
					c.recvByte += len(pkt.Raw)
				}
			}
		}
	})

	c.sub.pc.OnDataChannel(func(dc *webrtc.DataChannel) {
		log.Debugf("id=%v [c.sub.pc.OnDataChannel] got dc %v", c.ID, dc.Label())
		if dc.Label() == API_CHANNEL {
			log.Debugf("%v got dc %v", c.ID, dc.Label())
			c.sub.api = dc
			// send cmd after open
			c.sub.api.OnOpen(func() {
				if len(c.apiQueue) > 0 {
					for _, cmd := range c.apiQueue {
						log.Debugf("%v c.sub.api.OnOpen send cmd=%v", c.ID, cmd)
						marshalled, err := json.Marshal(cmd)
						if err != nil {
							continue
						}
						err = c.sub.api.Send(marshalled)
						if err != nil {
							log.Errorf("id=%v err=%v", c.ID, err)
						}
						time.Sleep(time.Millisecond * 10)
					}
					c.apiQueue = []Call{}
				}
			})
			return
		}
		log.Debugf("%v got dc %v", c.ID, dc.Label())
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
	err = c.signal.Join(sid, offer)
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
	log.Debugf("id=%v", c.ID)
	close(c.notify)
	if c.pub != nil {
		c.pub.pc.Close()
	}
	if c.sub != nil {
		c.sub.pc.Close()
	}
}

// CreateDataChannel create a custom datachannel
func (c *Client) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	log.Debugf("id=%v CreateDataChannel %v", c.ID, label)
	return c.pub.pc.CreateDataChannel(label, &webrtc.DataChannelInit{})
}

// Trickle receive candidate from sfu and add to pc
func (c *Client) Trickle(candidate webrtc.ICECandidateInit, target int) {
	log.Debugf("id=%v candidate=%v target=%v", c.ID, candidate, target)
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
			log.Errorf("id=%v err=%v", c.ID, err)
		}
	}

}

// Negotiate sub negotiate
func (c *Client) Negotiate(sdp webrtc.SessionDescription) error {
	log.Debugf("id=%v Negotiate sdp=%v", c.ID, sdp)
	// 1.sub set remote sdp
	err := c.sub.pc.SetRemoteDescription(sdp)
	if err != nil {
		log.Errorf("id=%v Negotiate c.sub.pc.SetRemoteDescription err=%v", c.ID, err)
		return err
	}

	// 2. safe to send candiate to sfu after join ok
	if len(c.sub.SendCandidates) > 0 {
		for _, cand := range c.sub.SendCandidates {
			log.Debugf("id=%v send sub.SendCandidates c.ID, c.signal.Trickle cand=%v", c.ID, cand)
			c.signal.Trickle(cand, SUBSCRIBER)
		}
		c.sub.SendCandidates = []*webrtc.ICECandidate{}
	}

	// 3. safe to add candidate after SetRemoteDescription
	if len(c.sub.RecvCandidates) > 0 {
		for _, candidate := range c.sub.RecvCandidates {
			log.Debugf("id=%v Negotiate c.sub.pc.AddICECandidate candidate=%v", c.ID, candidate)
			_ = c.sub.pc.AddICECandidate(candidate)
		}
		c.sub.RecvCandidates = []webrtc.ICECandidateInit{}
	}

	// 4. create answer after add ice candidate
	answer, err := c.sub.pc.CreateAnswer(nil)
	if err != nil {
		log.Errorf("id=%v err=%v", c.ID, err)
		return err
	}

	// 5. set local sdp(answer)
	err = c.sub.pc.SetLocalDescription(answer)
	if err != nil {
		log.Errorf("id=%v err=%v", c.ID, err)
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
		log.Errorf("id=%v err=%v", c.ID, err)
	}

	// 2. pub set local sdp(offer)
	err = c.pub.pc.SetLocalDescription(offer)
	if err != nil {
		log.Errorf("id=%v err=%v", c.ID, err)
	}

	log.Debugf("id=%v OnNegotiationNeeded!! c.pub.pc.CreateOffer and send offer=%v", c.ID, offer)
	//3. send offer to sfu
	c.signal.Offer(offer)
}

// selectRemote select remote video/audio
func (c *Client) selectRemote(streamId, video string, audio bool) error {
	log.Debugf("id=%v streamId=%v video=%v audio=%v", c.ID, streamId, video, audio)
	call := Call{
		StreamID: streamId,
		Video:    video,
		Audio:    audio,
	}

	// cache cmd when dc not ready
	if c.sub.api == nil || c.sub.api.ReadyState() != webrtc.DataChannelStateOpen {
		log.Debugf("id=%v append to c.apiQueue call=%v", c.ID, call)
		c.apiQueue = append(c.apiQueue, call)
		return nil
	}

	// send cached cmd
	if len(c.apiQueue) > 0 {
		for _, cmd := range c.apiQueue {
			log.Debugf("id=%v c.sub.api.Send cmd=%v", c.ID, cmd)
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
	log.Debugf("id=%v c.sub.api.Send call=%v", c.ID, call)
	marshalled, err := json.Marshal(call)
	if err != nil {
		return err
	}
	err = c.sub.api.Send(marshalled)
	if err != nil {
		log.Errorf("id=%v err=%v", c.ID, err)
	}
	return err
}

// UnSubscribeAll unsubscribe all stream
func (c *Client) UnSubscribeAll() {
	c.streamLock.RLock()
	m := c.remoteStreamId
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Debugf("id=%v UnSubscribe remote streamid=%v", c.ID, streamId)
		c.selectRemote(streamId, "none", false)
	}
}

// SubscribeAll subscribe all stream with the same video/audio param
func (c *Client) SubscribeAll(video string, audio bool) {
	c.streamLock.RLock()
	m := c.remoteStreamId
	c.streamLock.RUnlock()
	for streamId := range m {
		log.Debugf("id=%v Subscribe remote streamid=%v", c.ID, streamId)
		c.selectRemote(streamId, video, audio)
	}
}

// PublishWebm publish a webm producer
func (c *Client) PublishWebm(file string, video, audio bool) error {
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		c.producer = NewWebMProducer(c.ID, file, 0)
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
