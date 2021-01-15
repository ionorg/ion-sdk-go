package engine

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"sync"
	"time"

	log "github.com/pion/ion-log"
	sfu "github.com/pion/ion-sfu/cmd/signal/grpc/proto"
	"github.com/pion/webrtc/v3"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

var streamLock sync.Mutex

// SFU client
type SFU struct {
	ctx        context.Context
	cancel     context.CancelFunc
	client     sfu.SFUClient
	config     Config
	mu         sync.RWMutex
	onCloseFn  func()
	transports map[string]*WebRTCTransport
}

// NewSFU intializes a new SFU client
func NewSFU(addr string, config Config) (*SFU, error) {
	log.Infof("Connecting to sfu: %s", addr)
	// Set up a connection to the sfu server.
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Errorf("did not connect: %v", err)
		return nil, err
	}

	ctx, cancel := context.WithCancel(context.Background())
	return &SFU{
		ctx:        ctx,
		cancel:     cancel,
		client:     sfu.NewSFUClient(conn),
		config:     config,
		transports: make(map[string]*WebRTCTransport),
	}, nil
}

// GetTransport returns a webrtc transport for a session
func (s *SFU) GetTransport(sid, file string) (*WebRTCTransport, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	t := s.transports[sid]

	// no transport yet, create one
	if t == nil {
		var err error
		if t, err = s.join(sid, file); err != nil {
			return nil, err
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
		// t.OnPublish(s.Publish)
		s.transports[sid] = t
	}

	return t, nil
}

// OnClose handler called when sfu client is closed
func (s *SFU) OnClose(f func()) {
	s.onCloseFn = f
}

// Join creates an sfu client and join the session.
func (s *SFU) join(sid, file string) (*WebRTCTransport, error) {
	log.Infof("Joining sfu session: %s", sid)

	sfustream, err := s.client.Signal(s.ctx)

	if err != nil {
		log.Errorf("error creating sfu stream: %s", err)
		return nil, err
	}

	t := NewWebRTCTransport(sid, file, s.config)

	t.OnICECandidate(func(c *webrtc.ICECandidate, target int) {
		if c == nil {
			// Gathering done
			log.Infof("gather candidate done")
			return
		}
		bytes, err := json.Marshal(c.ToJSON())
		if err != nil {
			log.Errorf("OnIceCandidate error %s", err)
		}
		log.Infof("send ice candidate=%v", string(bytes))
		streamLock.Lock()
		err = sfustream.Send(&sfu.SignalRequest{
			Payload: &sfu.SignalRequest_Trickle{
				Trickle: &sfu.Trickle{
					Init:   string(bytes),
					Target: sfu.Trickle_Target(target),
				},
			},
		})
		streamLock.Unlock()
		if err != nil {
			log.Errorf("OnIceCandidate error %s", err)
		}
	})

	t.OnNegotiationNeeded(func() {
		log.Infof("t.OnNegotiationNeeded")
		offer, err := t.CreateOffer()
		if err != nil {
			log.Errorf("Error creating offer: %v", err)
			return
		}

		marshalled, err := json.Marshal(offer)
		if err != nil {
			return
		}

		log.Infof("Send offer:\n %s", offer.SDP)
		// streamLock.Lock()
		err = sfustream.Send(
			&sfu.SignalRequest{
				Payload: &sfu.SignalRequest_Description{
					Description: marshalled,
				},
			},
		)
		// streamLock.Unlock()

		if err != nil {
			log.Errorf("Error sending publish request: %v", err)
		}

	})

	offer, err := t.CreateOffer()
	if err != nil {
		log.Errorf("Error creating offer: %v", err)
		return nil, err
	}

	marshalled, err := json.Marshal(offer)
	if err != nil {
		return nil, err
	}

	log.Infof("Send offer:\n %s", offer.SDP)
	// streamLock.Lock()
	err = sfustream.Send(
		&sfu.SignalRequest{
			Payload: &sfu.SignalRequest_Join{
				Join: &sfu.JoinRequest{
					Sid:         sid,
					Description: marshalled,
				},
			},
		},
	)
	// streamLock.Unlock()

	if err != nil {
		log.Errorf("Error sending publish request: %v", err)
		return nil, err
	}

	go func() {
		// Handle sfu stream messages
		for {
			// streamLock.Lock()
			res, err := sfustream.Recv()
			// streamLock.Unlock()

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
				log.Infof("got answer: %s", payload.Join.Description)

				var sdp webrtc.SessionDescription
				err := json.Unmarshal(payload.Join.Description, &sdp)
				if err != nil {
					log.Errorf("sdp unmarshal error: %v", err)
					return
				}

				if err = t.SetRemoteDescription(sdp); err != nil {
					log.Errorf("join error %s", err)
					return
				}

			case *sfu.SignalReply_Description:
				var sdp webrtc.SessionDescription
				err := json.Unmarshal(payload.Description, &sdp)
				if err != nil {
					log.Errorf("sdp unmarshal error: %v", err)
					return
				}

				if sdp.Type == webrtc.SDPTypeOffer {
					log.Debugf("got offer: %v", sdp)

					log.Infof("dddddddddddddddddd=%+v", sdp)
					var answer webrtc.SessionDescription
					answer, err = t.Answer(sdp)
					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}

					marshalled, err = json.Marshal(answer)
					if err != nil {
						log.Errorf("sdp marshall error %s", err)
						continue
					}

					log.Infof("sssssssssssssssssss=%+v", answer)
					// streamLock.Lock()
					err = sfustream.Send(&sfu.SignalRequest{
						Payload: &sfu.SignalRequest_Description{
							Description: marshalled,
						},
					})
					// streamLock.Unlock()

					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}
				} else if sdp.Type == webrtc.SDPTypeAnswer {
					log.Debugf("got answer: %v", sdp)
					err = t.SetRemoteDescription(sdp)

					if err != nil {
						log.Errorf("negotiate error %s", err)
						continue
					}
				}
			case *sfu.SignalReply_Trickle:
				var candidate webrtc.ICECandidateInit
				_ = json.Unmarshal([]byte(payload.Trickle.Init), &candidate)
				log.Infof("type=%v candidate=%v", payload.Trickle.Target, candidate)
				err := t.AddICECandidate(candidate, int(payload.Trickle.Target))
				if err != nil {
					log.Errorf("error adding ice candidate: %e", err)
				}
			default:
				log.Errorf("Unknow signal type!!!!%v", payload)
			}
		}
	}()

	return t, nil
}

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
