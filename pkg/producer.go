package engine

import "github.com/pion/webrtc/v3"

//producer interface
type producer interface {
	Start()
	Stop()
	AddTrack(pc *webrtc.PeerConnection, kind string) (*webrtc.TrackLocalStaticSample, error)
	GetSendBandwidth(cycle int) int
}
