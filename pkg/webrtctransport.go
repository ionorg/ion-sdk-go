package engine

import (
	"errors"
	"math/rand"
	"path/filepath"

	log "github.com/pion/ion-log"
	"github.com/pion/webrtc/v3"
)

var (
	errInvalidFile = errors.New("invalid file")
)

// WebRTCTransportConfig represents configuration options
type WebRTCTransportConfig struct {
	Configuration webrtc.Configuration
	Setting       webrtc.SettingEngine
}

// WebRTCTransport represents a webrtc transport
type WebRTCTransport struct {
	id        string
	pc        *webrtc.PeerConnection
	dc        *webrtc.DataChannel
	recvByte  int
	onCloseFn func()
	tracks    []*webrtc.Track
	producer  producer
}

// NewWebRTCTransport creates a new webrtc transport
func NewWebRTCTransport(id string, cfg WebRTCTransportConfig) *WebRTCTransport {
	// Create peer connection
	me := webrtc.MediaEngine{}
	me.RegisterDefaultCodecs()
	api := webrtc.NewAPI(webrtc.WithMediaEngine(me), webrtc.WithSettingEngine(cfg.Setting))
	log.Debugf("cfg.Configuration=%+v", cfg.Configuration)
	pc, err := api.NewPeerConnection(cfg.Configuration)

	if err != nil {
		log.Errorf("Error creating peer connection: %s", err)
		return nil
	}

	dc, err := pc.CreateDataChannel("ion-sfu", nil)
	if err != nil {
		log.Errorf("Error creating peer data channel: %s", err)
		return nil
	}

	t := &WebRTCTransport{
		id: id,
		pc: pc,
		dc: dc,
	}

	return t
}

// OnClose sets a handler that is called when the webrtc transport is closed
func (t *WebRTCTransport) OnClose(f func()) {
	t.onCloseFn = f
}

// Close the webrtc transport
func (t *WebRTCTransport) Close() error {
	if t.onCloseFn != nil {
		t.onCloseFn()
	}
	if t.producer != nil {
		t.producer.Stop()
	}
	return t.pc.Close()
}

func (t *WebRTCTransport) OnTrack(f func(track *webrtc.Track, recv *webrtc.RTPReceiver)) {
	t.pc.OnTrack(f)
}

// CreateOffer starts the PeerConnection and generates the localDescription
func (t *WebRTCTransport) CreateOffer() (webrtc.SessionDescription, error) {
	if len(t.tracks) == 0 {
		track, err := t.pc.NewTrack(webrtc.DefaultPayloadTypeVP8, rand.Uint32(), "video", "pion")
		if err != nil {
			return webrtc.SessionDescription{}, err
		}
		t.pc.AddTrack(track)
		t.tracks = append(t.tracks, track)
		track, err = t.pc.NewTrack(webrtc.DefaultPayloadTypeOpus, rand.Uint32(), "audio", "pion")
		if err != nil {
			return webrtc.SessionDescription{}, err
		}
		t.pc.AddTrack(track)
		t.tracks = append(t.tracks, track)
	}
	log.Infof("t.tracks=%v", t.tracks)
	return t.pc.CreateOffer(nil)
}

// CreateAnswer starts the PeerConnection and generates the localDescription
func (t *WebRTCTransport) CreateAnswer() (webrtc.SessionDescription, error) {
	return t.pc.CreateAnswer(nil)
}

// SetLocalDescription sets the SessionDescription of the local peer
func (t *WebRTCTransport) SetLocalDescription(desc webrtc.SessionDescription) error {
	return t.pc.SetLocalDescription(desc)
}

// SetRemoteDescription sets the SessionDescription of the remote peer
func (t *WebRTCTransport) SetRemoteDescription(desc webrtc.SessionDescription) error {
	return t.pc.SetRemoteDescription(desc)
}

// AddICECandidate accepts an ICE candidate string and adds it to the existing set of candidates
func (t *WebRTCTransport) AddICECandidate(candidate webrtc.ICECandidateInit) error {
	return t.pc.AddICECandidate(candidate)
}

// OnICECandidate sets an event handler which is invoked when a new ICE candidate is found.
// Take note that the handler is gonna be called with a nil pointer when gathering is finished.
func (t *WebRTCTransport) OnICECandidate(f func(c *webrtc.ICECandidate)) {
	t.pc.OnICECandidate(f)
}

// OnICEConnectionStateChange sets an event handler which is invoked when ICE connection state changed.
func (t *WebRTCTransport) OnICEConnectionStateChange(f func(c webrtc.ICEConnectionState)) {
	t.pc.OnICEConnectionStateChange(f)
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
	track, err := t.producer.AddTrack(t.pc, "video")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	t.tracks = append(t.tracks, track)
	track, err = t.producer.AddTrack(t.pc, "audio")
	if err != nil {
		log.Infof("err=%v", err)
		return err
	}
	t.tracks = append(t.tracks, track)
	t.producer.Start()
	return nil
}

// OnConsume read rtp and drop
func (t *WebRTCTransport) Subscribe() {
	t.OnTrack(func(track *webrtc.Track, recv *webrtc.RTPReceiver) {
		log.Infof("OnTrack: %v", track)
		var lastNum uint16
		for {
			// Discard packet
			packet, err := track.ReadRTP()
			t.recvByte += packet.MarshalSize()
			if err != nil {
				log.Errorf("Error reading RTP packet %v", err)
				return
			}
			seq := packet.Header.SequenceNumber
			if seq != lastNum+1 {
				// log.Infof("Packet out of order! prev %d current %d", lastNum, seq)
			}
			lastNum = seq
		}
	})
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
