package engine

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/pion/webrtc/v3"
)

const (
	API_CHANNEL = "ion-sfu"
)

//Call dc api
type Call struct {
	StreamID string `json:"streamId"`
	Video    string `json:"video"`
	Audio    bool   `json:"audio"`
}

type VideoInfo struct {
	Width     uint32
	Height    uint32
	FrameRate uint32
	// Simulcast
	// {'f': 'send', 'h': 'send', 'q': 'send'}
	// {'f': 'recv', 'h': 'recv', 'q': 'recv'}
	// Simulcast map[string]string
	Layer     string
	Direction string
}

type TrackInfo struct {
	Id        string
	Kind      string
	Muted     bool
	Type      MediaType
	StreamId  string
	Label     string
	Subscribe bool
	Layer     string
	Direction string
	Width     uint32
	Height    uint32
	FrameRate uint32
}

type Subscription struct {
	TrackId   string
	Mute      bool
	Subscribe bool
	Layer     string
}

type Target int32

const (
	Target_PUBLISHER  Target = 0
	Target_SUBSCRIBER Target = 1
)

type MediaType int32

const (
	MediaType_MediaUnknown  MediaType = 0
	MediaType_UserMedia     MediaType = 1
	MediaType_ScreenCapture MediaType = 2
	MediaType_Cavans        MediaType = 3
	MediaType_Streaming     MediaType = 4
	MediaType_VoIP          MediaType = 5
)

type TrackEvent_State int32

const (
	TrackEvent_ADD    TrackEvent_State = 0
	TrackEvent_UPDATE TrackEvent_State = 1
	TrackEvent_REMOVE TrackEvent_State = 2
)

// TrackEvent info
type TrackEvent struct {
	State TrackEvent_State
	// user id
	Uid    string
	Tracks []*TrackInfo
}

type ClientConfig struct {
	Addr  string
	Sid   string
	Uid   string
	Token string
}

// Client a sdk client
type Client struct {
	uid    string
	sid    string
	pub    *Transport
	sub    *Transport
	signal *Signal

	//export to user
	OnTrack       func(track *webrtc.TrackRemote, receiver *webrtc.RTPReceiver)
	OnDataChannel func(*webrtc.DataChannel)
	OnError       func(error)
	OnTrackEvent  func(event TrackEvent)
	OnSpeaker     func(event []string)

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

// Join client join a session
func (c *Client) Join(sid string, config ...JoinConfig) error {
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

	c.sub.pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		if state >= webrtc.ICEConnectionStateDisconnected {
			log.Infof("[c.sub.pc.OnICEConnectionStateChange] delClient %v", c)
			c.engine.delClient(c)
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

	if len(config) > 0 {
		err = c.signal.Join(sid, c.uid, offer, config[0])
	} else {
		err = c.signal.Join(sid, c.uid, offer, nil)
	}

	if err != nil {
		return err
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

// Publish local tracks
func (c *Client) Publish(tracks ...webrtc.TrackLocal) ([]*webrtc.RTPTransceiver, error) {
	var transceivers []*webrtc.RTPTransceiver
	for _, t := range tracks {
		if _, err := c.pub.GetPeerConnection().AddTrack(t); err != nil {
			log.Errorf("AddTrack error: %v", err)
			return transceivers, err
		}
	}
	c.onNegotiationNeeded()
	return transceivers, nil
}

// UnPublish local tracks by transceivers
func (c *Client) UnPublish(transceivers ...*webrtc.RTPTransceiver) error {
	for _, t := range transceivers {
		if err := c.pub.pc.RemoveTrack(t.Sender()); err != nil {
			return err
		}
	}
	c.onNegotiationNeeded()
	return nil
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
}

// CreateDataChannel create a custom datachannel
func (c *Client) CreateDataChannel(label string) (*webrtc.DataChannel, error) {
	log.Debugf("id=%v CreateDataChannel %v", c.uid, label)
	return c.pub.pc.CreateDataChannel(label, &webrtc.DataChannelInit{})
}

// trickle receive candidate from sfu and add to pc
func (c *Client) trickle(candidate webrtc.ICECandidateInit, target int) {
	log.Debugf("id=%v candidate=%v target=%v", c.uid, candidate, target)
	var t *Transport
	if target == int(Target_SUBSCRIBER) {
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

// negotiate sub negotiate
func (c *Client) negotiate(sdp webrtc.SessionDescription) error {
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
			log.Debugf("id=%v send sub.SendCandidates c.uid, c.signal.trickle cand=%v", c.uid, cand)
			c.signal.trickle(cand, Target_SUBSCRIBER)
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
	err = c.signal.answer(answer)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
		return err
	}
	return err
}

// onNegotiationNeeded will be called when add/remove track, but never trigger, call by hand
func (c *Client) onNegotiationNeeded() {
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

	//3. send offer to sfu
	err = c.signal.offer(offer)
	if err != nil {
		log.Errorf("id=%v err=%v", c.uid, err)
	}
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
				log.Errorf("error: %v", err)
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

// PublishWebm publish a webm producer
func (c *Client) PublishFile(file string, video, audio bool) error {
	if !FileExist(file) {
		return os.ErrNotExist
	}
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		c.producer = NewWebMProducer(file, 0)
	default:
		return errInvalidFile
	}
	if video {
		videoTrack, err := c.producer.GetVideoTrack()
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
		_, err = c.pub.pc.AddTrack(videoTrack)
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
	}
	if audio {
		audioTrack, err := c.producer.GetAudioTrack()
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
		_, err = c.pub.pc.AddTrack(audioTrack)
		if err != nil {
			log.Debugf("error: %v", err)
			return err
		}
	}
	c.producer.Start()
	//trigger by hand
	c.onNegotiationNeeded()
	return nil
}

// Subscribe to tracks
func (c *Client) Subscribe(infos []*Subscription) error {
	return c.signal.Subscribe(infos)
}

func (c *Client) trackEvent(event TrackEvent) {
	if c.OnTrackEvent == nil {
		log.Errorf("c.OnTrackEvent == nil")
		return
	}
	c.OnTrackEvent(event)
}

func (c *Client) speaker(event []string) {
	if c.OnSpeaker == nil {
		log.Errorf("c.OnSpeaker == nil")
		return
	}
	c.OnSpeaker(event)
}

// setRemoteSDP pub SetRemoteDescription and send cadidate to sfu
func (c *Client) setRemoteSDP(sdp webrtc.SessionDescription) error {
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
			log.Debugf("id=%v c.signal.trickle cand=%v", c.uid, cand)
			c.signal.trickle(cand, Target_PUBLISHER)
		}
		c.pub.SendCandidates = []*webrtc.ICECandidate{}
	}
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
