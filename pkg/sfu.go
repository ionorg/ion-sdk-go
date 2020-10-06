package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	log "github.com/pion/ion-log"
	sfu "github.com/pion/ion-sfu/cmd/server/grpc/proto"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// SFU client
type SFU struct {
	ctx        context.Context
	cancel     context.CancelFunc
	client     sfu.SFUClient
	config     WebRTCTransportConfig
	mu         sync.RWMutex
	onCloseFn  func()
	transports map[string]*WebRTCTransport
	stop       bool
}

// NewSFU intializes a new SFU client
func NewSFU(addr string, config WebRTCTransportConfig) *SFU {
	log.Infof("Connecting to sfu: %s", addr)
	// Set up a connection to the sfu server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Errorf("did not connect: %v", err)
		return nil
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &SFU{
		ctx:        ctx,
		cancel:     cancel,
		client:     sfu.NewSFUClient(conn),
		config:     config,
		transports: make(map[string]*WebRTCTransport),
	}
}

// GetTransport returns a webrtc transport for a session
func (s *SFU) GetTransport(sid, tid string) *WebRTCTransport {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.transports[tid]
	if t != nil {
		return t
	}

	// no transport yet, create one
	t = s.NewWebRTCTransport(tid)
	t.OnClose(func() {
		s.mu.Lock()
		defer s.mu.Unlock()
		delete(s.transports, tid)
		if len(s.transports) == 0 && s.onCloseFn != nil {
			s.cancel()
			s.onCloseFn()
		}
	})
	s.transports[tid] = t
	return t
}

// GetTransportWithWebmProducer returns a webrtc transport for a session
func (s *SFU) GetTransportWithWebmProducer(sid, file, producer string) (*WebRTCTransport, *WebMProducer) {
	s.mu.Lock()
	defer s.mu.Unlock()
	t := s.transports[sid]

	// no transport yet, create one
	var webm *WebMProducer
	if t == nil {
		t = s.NewWebRTCTransport(sid)
		t.AddProducer(file)
		// webm = s.AttachWebMProducer(t, file)
		err := s.Join(sid, t)
		if err != nil {
			return nil, nil
		}
		t.OnClose(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.transports, sid)
			if len(s.transports) == 0 && s.onCloseFn != nil {
				s.cancel()
				s.onCloseFn()
			}
		})
		s.transports[sid] = t
	}

	return t, webm
}

// OnClose handler called when sfu client is closed
func (s *SFU) OnClose(f func()) {
	s.onCloseFn = f
}

func (s *SFU) Close() {
	for _, t := range s.transports {
		// Signal shutdown
		t.Close()
		time.Sleep(200 * time.Millisecond)
	}
}

// NewWebRTCTransport ..
func (s *SFU) NewWebRTCTransport(id string) *WebRTCTransport {
	return NewWebRTCTransport(id, s.config)
}

// Join join the session.
func (s *SFU) Join(sid string, t *WebRTCTransport) error {
	log.Infof("joining sfu session: %s", sid)

	sfustream, err := s.client.Signal(s.ctx)

	if err != nil {
		log.Errorf("error creating sfu stream: %s", err)
		return err
	}

	t.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			// Gathering done
			return
		}
		bytes, err := json.Marshal(c.ToJSON())
		if err != nil {
			log.Errorf("OnIceCandidate error %s", err)
		}
		log.Debugf("t.OnICECandidate %v", c.ToJSON())
		//make sure send offer first
		time.Sleep(100 * time.Millisecond)
		err = sfustream.Send(&sfu.SignalRequest{
			Payload: &sfu.SignalRequest_Trickle{
				Trickle: &sfu.Trickle{
					Init: string(bytes),
				},
			},
		})
		if err != nil {
			log.Errorf("OnIceCandidate error %s", err)
		}
	})

	// t.OnTrack(func(track *webrtc.Track, recv *webrtc.RTPReceiver) {
	// id := track.ID()
	// log.Infof("Got track: %s", id)
	// var lastNum uint16
	// for {
	// // Discard packet
	// // Do nothing
	// packet, err := track.ReadRTP()
	// if err != nil {
	// log.Errorf("Error reading RTP packet %v\n", err)
	// return
	// }
	// seq := packet.Header.SequenceNumber
	// if seq != lastNum+1 {
	// log.Errorf("Packet out of order! prev %d current %d\n", lastNum, seq)
	// }
	// lastNum = seq
	// }
	// })

	offer, err := t.CreateOffer()
	if err != nil {
		log.Errorf("Error creating offer: %v", err)
		return err
	}

	if err = t.SetLocalDescription(offer); err != nil {
		log.Errorf("Error setting local description: %v", err)
		return err
	}
	log.Debugf("Send offer:\n %s", offer.SDP)
	err = sfustream.Send(
		&sfu.SignalRequest{
			Payload: &sfu.SignalRequest_Join{
				Join: &sfu.JoinRequest{
					Sid: sid,
					Offer: &sfu.SessionDescription{
						Type: offer.Type.String(),
						Sdp:  []byte(offer.SDP),
					},
				},
			},
		},
	)

	if err != nil {
		log.Errorf("Error sending join request: %v", err)
		return err
	}

	go func() {
		// Handle sfu stream messages
		for {
			res, err := sfustream.Recv()

			if err != nil {
				if err == io.EOF {
					// WebRTC Transport closed
					log.Infof("WebRTC Transport Closed")
					err = sfustream.CloseSend()
					if err != nil {
						log.Errorf("error sending close: %s", err)
					}
					return
				}

				errStatus, _ := status.FromError(err)
				if errStatus.Code() == codes.Canceled {
					err = sfustream.CloseSend()
					if err != nil {
						log.Errorf("error sending close: %s", err)
					}
					return
				}

				log.Errorf("Error receiving signal response: %v", err)
				return
			}

			switch payload := res.Payload.(type) {
			case *sfu.SignalReply_Join:
				// Set the remote SessionDescription
				log.Debugf("got answer: %s", string(payload.Join.Answer.Sdp))
				if err = t.SetRemoteDescription(webrtc.SessionDescription{
					Type: webrtc.SDPTypeAnswer,
					SDP:  string(payload.Join.Answer.Sdp),
				}); err != nil {
					log.Errorf("join error %s", err)
					return
				}

			case *sfu.SignalReply_Negotiate:
				log.Debugf("got negotiate %s", payload.Negotiate.Type)
				if payload.Negotiate.Type == webrtc.SDPTypeOffer.String() {
					log.Debugf("got offer: %s", string(payload.Negotiate.Sdp))
					offer := webrtc.SessionDescription{
						Type: webrtc.SDPTypeOffer,
						SDP:  string(payload.Negotiate.Sdp),
					}

					// Peer exists, renegotiating existing peer
					err = t.SetRemoteDescription(offer)
					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}

					var answer webrtc.SessionDescription
					answer, err = t.CreateAnswer()
					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}

					log.Infof("answer=%v", answer)
					err = t.SetLocalDescription(answer)
					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}

					err = sfustream.Send(&sfu.SignalRequest{
						Payload: &sfu.SignalRequest_Negotiate{
							Negotiate: &sfu.SessionDescription{
								Type: answer.Type.String(),
								Sdp:  []byte(answer.SDP),
							},
						},
					})

					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}
				} else if payload.Negotiate.Type == webrtc.SDPTypeAnswer.String() {
					log.Debugf("got answer: %s", string(payload.Negotiate.Sdp))
					err = t.SetRemoteDescription(webrtc.SessionDescription{
						Type: webrtc.SDPTypeAnswer,
						SDP:  string(payload.Negotiate.Sdp),
					})

					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}
				}
			case *sfu.SignalReply_Trickle:
				var candidate webrtc.ICECandidateInit
				_ = json.Unmarshal([]byte(payload.Trickle.Init), &candidate)
				log.Debugf("got trickle candidate=%v", candidate)
				err := t.AddICECandidate(candidate)
				if err != nil {
					log.Errorf("error adding ice candidate: %e", err)
				}
			}
		}
	}()

	return nil
}

// AttachWebMProducer attach WebMProducer to transport
// func (s *SFU) AttachWebMProducer(t *WebRTCTransport, file string) *WebMProducer {
// if file == "" {
// return nil
// }
// webm.AttachVideoTrackToPC(t.pc)
// webm.AttachAudioTrackToPC(t.pc)
// return webm
// }

// Stats show all sfu client stats
func (s *SFU) Stats(cycle int) string {
	for {
		info := "\n-------stats-------\n"

		s.mu.RLock()
		if len(s.transports) == 0 {
			s.mu.RUnlock()
			continue
		}
		info += fmt.Sprintf("Transport: %d\n", len(s.transports))

		totalRecvBW, totalSendBW := 0, 0
		for _, transport := range s.transports {
			recvBW, sendBW := transport.GetBandWidth(cycle)
			totalRecvBW += recvBW
			totalSendBW += sendBW
		}

		info += fmt.Sprintf("RecvBandWidth: %d KB/s\n", totalRecvBW)
		info += fmt.Sprintf("SendBandWidth: %d KB/s\n", totalSendBW)
		s.mu.RUnlock()
		log.Infof(info)
		time.Sleep(time.Duration(cycle) * time.Second)
	}
}
