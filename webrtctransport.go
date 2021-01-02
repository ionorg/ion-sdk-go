package engine

import (
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sync"

	log "github.com/pion/ion-log"
	"github.com/pion/rtcp"
	"github.com/pion/rtp"

	"github.com/pion/webrtc/v3"
)

var (
	errInvalidSubscriber = errors.New("invalid subscriber")
)

const (
	publisher  = 0
	subscriber = 1
)

// WebRTCTransportConfig represents configuration options
type WebRTCTransportConfig struct {
	Configuration webrtc.Configuration
	Setting       webrtc.SettingEngine
}

type SFUFeedback struct {
	StreamID string `json:"streamId"`
	Video    string `json:"video"`
	Audio    bool   `json:"audio"`
}

// WebRTCTransport represents a webrtc transport
type WebRTCTransport struct {
	id  string
	pub *Publisher
	sub *Subscriber
	mu  sync.RWMutex

	onCloseFn func()
	producer  *WebMProducer
	recvByte  int
	notify    chan struct{}
}

// NewWebRTCTransport creates a new webrtc transport
func NewWebRTCTransport(id string, c Config) *WebRTCTransport {
	conf := webrtc.Configuration{}
	se := webrtc.SettingEngine{}

	var icePortStart, icePortEnd uint16

	if len(c.WebRTC.ICEPortRange) == 2 {
		icePortStart = c.WebRTC.ICEPortRange[0]
		icePortEnd = c.WebRTC.ICEPortRange[1]
	}

	if icePortStart != 0 || icePortEnd != 0 {
		if err := se.SetEphemeralUDPPortRange(icePortStart, icePortEnd); err != nil {
			panic(err)
		}
	}

	var iceServers []webrtc.ICEServer
	for _, iceServer := range c.WebRTC.ICEServers {
		s := webrtc.ICEServer{
			URLs:       iceServer.URLs,
			Username:   iceServer.Username,
			Credential: iceServer.Credential,
		}
		iceServers = append(iceServers, s)
	}

	conf.ICEServers = iceServers

	config := WebRTCTransportConfig{
		setting:       se,
		configuration: conf,
	}

	pub, err := NewPublisher(config)
	if err != nil {
		log.Errorf("Error creating peer connection: %s", err)
		return nil
	}

	sub, err := NewSubscriber(config)
	if err != nil {
		log.Errorf("Error creating peer connection: %s", err)
		return nil
	}

	t := &WebRTCTransport{
		id:     id,
		pub:    pub,
		sub:    sub,
		notify: make(chan struct{}),
	}

	return t
}

// OnClose sets a handler that is called when the webrtc transport is closed
func (t *WebRTCTransport) OnClose(f func()) {
	t.onCloseFn = f
}

// Close the webrtc transport
func (t *WebRTCTransport) Close() error {
	t.mu.Lock()
	defer t.mu.Unlock()

	if t.onCloseFn != nil {
		t.onCloseFn()
	}

	err := t.sub.Close()
	if err != nil {
		return err
	}
	return t.pub.Close()
}

// CreateOffer starts the PeerConnection and generates the localDescription
func (t *WebRTCTransport) CreateOffer() (webrtc.SessionDescription, error) {
	return t.pub.CreateOffer()
}

// SetRemoteDescription sets the SessionDescription of the remote peer
func (t *WebRTCTransport) SetRemoteDescription(desc webrtc.SessionDescription) error {
	return t.pub.SetRemoteDescription(desc)
}

// Answer starts the PeerConnection and generates the localDescription
func (t *WebRTCTransport) Answer(offer webrtc.SessionDescription) (webrtc.SessionDescription, error) {
	return t.sub.Answer(offer)
}

// AddICECandidate accepts an ICE candidate string and adds it to the existing set of candidates
func (t *WebRTCTransport) AddICECandidate(candidate webrtc.ICECandidateInit, target int) error {
	switch target {
	case publisher:
		if err := t.pub.AddICECandidate(candidate); err != nil {
			return fmt.Errorf("error setting ice candidate: %w", err)
		}
	case subscriber:
		if err := t.sub.AddICECandidate(candidate); err != nil {
			return fmt.Errorf("error setting ice candidate: %w", err)
		}
	}
	return nil
}

// OnICECandidate sets an event handler which is invoked when a new ICE candidate is found.
// Take note that the handler is gonna be called with a nil pointer when gathering is finished.
func (t *WebRTCTransport) OnICECandidate(f func(c *webrtc.ICECandidate, target int)) {
	t.pub.OnICECandidate(func(c *webrtc.ICECandidate) {
		f(c, publisher)
	})
	t.sub.OnICECandidate(func(c *webrtc.ICECandidate) {
		f(c, subscriber)
	})
}

// AddProducer add a webm or mp4 file
func (t *WebRTCTransport) AddProducer(file string) error {
	ext := filepath.Ext(file)
	switch ext {
	case ".webm":
		t.producer = NewWebMProducer(file, 0)
	default:
		return errInvalidFile
	}
	if t.pub == nil {
		return errors.New("invalid pub")
	}
	_, err := t.producer.AddTrack(t.pub.pc, "video")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	_, err = t.producer.AddTrack(t.pub.pc, "audio")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	t.producer.Start()
	return nil
}

func (t *WebRTCTransport) GetBandWidth(cycle int) (int, int) {
	var recvBW, sendBW int
	if t.producer != nil {
		sendBW = t.producer.GetSendBandwidth(cycle)
	}

	recvBW = t.recvByte / cycle / 1000
	t.recvByte = 0
	return recvBW, sendBW
}

func (t *WebRTCTransport) Subscribe(write func(pkt *rtp.Packet)) error {
	if t.sub == nil {
		return errInvalidSubscriber
	}
	t.sub.OnTrack(func(track *webrtc.TrackRemote, recv *webrtc.RTPReceiver) {
		id := track.ID()
		log.Infof("Got track: %s", id)
		t.mu.Lock()
		defer t.mu.Unlock()

		if track.Kind() == webrtc.RTPCodecTypeVideo {
			err := t.sub.pc.WriteRTCP([]rtcp.Packet{&rtcp.PictureLossIndication{SenderSSRC: uint32(track.SSRC()), MediaSSRC: uint32(track.SSRC())}})
			if err != nil {
				log.Errorf("error writing pli %s", err)
			}
		}

		for {
			select {
			case <-t.notify:
				return
			default:
				pkt, _, err := track.ReadRTP()
				if err != nil {
					if err == io.EOF {
						log.Errorf("track.ReadRTP err=%v", err)
						return
					}
					log.Errorf("Error reading track rtp %s", err)
					continue
				}
				if write != nil {
					write(pkt)
				}
			}
		}
	})
	return nil
}
