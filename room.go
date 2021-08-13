package engine

import (
	"context"
	"fmt"
	"io"
	"sync"

	room "github.com/pion/ion/apps/room/proto"
	"github.com/square/go-jose/v3/json"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type RoomClient struct {
	roomServiceClient room.RoomServiceClient
	roomSignalClient  room.RoomSignalClient
	roomSignalStream  room.RoomSignal_SignalClient
	// roomSignalClient            room.Room_SignalClient
	ctx    context.Context
	cancel context.CancelFunc
	sync.Mutex

	OnJoin      func(success bool, info RoomInfo, err error)
	OnLeave     func(success bool, err error)
	OnPeerEvent func(state PeerState, Peer PeerInfo)
	// OnStreamEvent      func(state StreamState, sid string, uid string, roomSignalClients []*Stream)
	OnMessage    func(from string, to string, data map[string]interface{})
	OnDisconnect func(sid, reason string)
	OnRoomInfo   func(info RoomInfo)
	OnError      func(error)
}

func GetError(err *room.Error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("[%d]: %s", err.Code, err.Reason)
}

func NewRoomClient(addr string) *RoomClient {
	c := &RoomClient{}
	conn, err := grpc.Dial(addr, grpc.WithInsecure(), grpc.WithBlock())
	if err != nil {
		log.Errorf("did not connect: %v", err)
		return nil
	}
	log.Infof("gRPC connected: %s", addr)

	c.ctx, c.cancel = context.WithCancel(context.Background())
	c.roomServiceClient = room.NewRoomServiceClient(conn)
	c.roomSignalClient = room.NewRoomSignalClient(conn)
	c.roomSignalStream, err = c.roomSignalClient.Signal(c.ctx)
	// c.roomSignalClient, err = c.client.Signal(c.ctx)
	// c.roomSignalClient, err = c.client.Signal(c.ctx)
	if err != nil {
		log.Errorf("err=%v", err)
		return nil
	}
	go c.roomSignalReadLoop()
	return c
}

// CreateRoom create a room
func (c *RoomClient) CreateRoom(ctx context.Context, req *room.CreateRoomRequest) (*room.CreateRoomReply, error) {
	return c.roomServiceClient.CreateRoom(c.ctx, req)
}

// EndRoom delete a room
func (c *RoomClient) EndRoom(ctx context.Context, req *room.EndRoomRequest) (*room.EndRoomReply, error) {
	return c.roomServiceClient.EndRoom(c.ctx, req)
}

// AddPeer add a Peer
func (c *RoomClient) AddPeer(ctx context.Context, req *room.AddPeerRequest) (*room.AddPeerReply, error) {
	return c.roomServiceClient.AddPeer(c.ctx, req)
}

// RemovePeer remove a Peer
func (c *RoomClient) RemovePeer(ctx context.Context, req *room.RemovePeerRequest) (*room.RemovePeerReply, error) {
	return c.roomServiceClient.RemovePeer(c.ctx, req)
}

// GetPeers get all Peers
func (c *RoomClient) GetPeers(ctx context.Context, req *room.GetPeersRequest) (*room.GetPeersReply, error) {
	return c.roomServiceClient.GetPeers(c.ctx, req)
}

// LockRoom lock/unlock a room, avoid someone join
func (c *RoomClient) UpdateRoom(ctx context.Context, req *room.UpdateRoomRequest) (*room.UpdateRoomReply, error) {
	return c.roomServiceClient.UpdateRoom(c.ctx, req)
}

// SetImportance ..
func (c *RoomClient) SetImportance(ctx context.Context, req *room.SetImportanceRequest) (*room.SetImportanceReply, error) {
	return c.roomServiceClient.SetImportance(c.ctx, req)
}

// EditPeerInfo ..
func (c *RoomClient) UpdatePeer(ctx context.Context, req *room.UpdatePeerRequest) (*room.UpdatePeerReply, error) {
	return c.roomServiceClient.UpdatePeer(c.ctx, req)
}

func (c *RoomClient) Close() {
	c.cancel()
	c.roomSignalStream.CloseSend()
	log.Infof("Close ok")
}

func (c *RoomClient) Join(sid string, uid string, info map[string]interface{}) error {
	log.Infof("sid=%v uid=%v, info=%v", sid, uid, info)
	buf, err := json.Marshal(info)
	if err != nil {
		log.Errorf("Marshal info [%v] err=%v", info, err)
		c.OnError(err)
		return err
	}
	err = c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_Join{
				Join: &room.JoinRequest{
					Sid:         sid,
					Uid:         uid,
					DisplayName: "ion-sdk-go",
					Role:        room.Role_Host,
					Password:    "",
					ExtraInfo:   buf,
				},
			},
		},
	)

	if err != nil {
		log.Errorf("[%v] err=%v", sid, err)
		c.OnError(err)
		return err
	}

	return nil
}

// Leave from one session
func (c *RoomClient) Leave(sid, uid string) error {
	log.Infof("uid=%v", uid)
	err := c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_Leave{
				Leave: &room.LeaveRequest{
					Sid: sid,
					Uid: uid,
				},
			},
		},
	)
	if err != nil {
		log.Errorf("[%v] err=%v", uid, err)
		c.OnError(err)
		return err
	}
	return nil
}

// SendMessage send message
func (c *RoomClient) SendMessage(sid, from, to string, data map[string]interface{}) error {
	log.Infof("[Room.SendMessage] from=%v, to=%v, data=%v", from, to, data)
	buf, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Marshal msg.data [%v] err=%v", from, err)
		c.OnError(err)
		return err
	}
	err = c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_SendMessage{
				&room.SendMessageRequest{
					Sid: sid,
					Message: &room.Message{
						From:    from,
						To:      to,
						Type:    "text",
						Payload: buf,
					},
				},
			},
		},
	)
	if err != nil {
		log.Errorf("err=%v", err)
		c.OnError(err)
		return err
	}

	return nil
}

func (c *RoomClient) roomSignalReadLoop() error {
	for {
		res, err := c.roomSignalStream.Recv()
		if err != nil {
			if err == io.EOF {
				if err := c.roomSignalStream.CloseSend(); err != nil {
					log.Errorf("error sending close: %s", err)
				}
				c.OnError(err)
				return err
			}

			errStatus, _ := status.FromError(err)
			if errStatus.Code() == codes.Canceled {
				if err := c.roomSignalStream.CloseSend(); err != nil {
					log.Errorf("error sending close: %s", err)
				}
				c.OnError(err)
				return err
			}

			log.Errorf("Error receiving room response: %v", err)
			c.OnError(err)
			return err
		}

		log.Infof("RoomClient.roomSignalReadLoop reply %v", res)

		switch payload := res.Payload.(type) {
		case *room.Reply_Join:
			reply := payload.Join
			log.Infof("reply=====%+v", reply)
			if c.OnJoin == nil {
				log.Errorf("c.OnJoin is nil")
				continue
			}
			if reply == nil || reply.Room == nil {
				c.OnJoin(false, RoomInfo{}, fmt.Errorf("replay is nil or reply.RoomInfo is nil"))
			}
			c.OnJoin(
				reply.Success,
				RoomInfo{
					Sid:  reply.Room.Sid,
					Name: reply.Room.Name,
					Lock: reply.Room.Lock,
				},
				GetError(reply.Error),
			)

		case *room.Reply_Leave:
			reply := payload.Leave
			if c.OnLeave != nil {
				c.OnLeave(reply.Success, GetError(reply.Error))
			}
		case *room.Reply_Message:
			msg := payload.Message
			data := make(map[string]interface{})

			err := json.Unmarshal(msg.Payload, &data)
			if err != nil {
				log.Errorf("Unmarshal: err %v", err)
				c.OnError(err)
				return err
			}

			if c.OnMessage != nil {
				c.OnMessage(msg.From, msg.To, data)
			}
		case *room.Reply_Peer:
			event := payload.Peer
			var trackInfos []TrackInfo
			if event.State == room.PeerState_JOIN ||
				event.State == room.PeerState_LEAVE {
				for _, track := range event.Peer.Tracks {
					trackInfos = append(
						trackInfos,
						TrackInfo{
							Id:       track.Id,
							StreamId: track.StreamId,
						},
					)
				}
				if err != nil {
					log.Errorf("Unmarshal peer.info: err %v", err)
					c.OnError(err)
					return err
				}
			}

			if c.OnPeerEvent != nil {
				c.OnPeerEvent(
					PeerState(event.State),
					PeerInfo{
						Sid:    event.Peer.Sid,
						Uid:    event.Peer.Uid,
						Tracks: trackInfos,
					},
				)
			}
		case *room.Reply_MediaPresentation:
			event := payload.MediaPresentation
			log.Info("room.MediaPresentation: %+v", event)
		case *room.Reply_Disconnect:
			event := payload.Disconnect
			log.Info("room.Disconnect: %+v", event)
			if c.OnDisconnect != nil {
				c.OnDisconnect(event.Sid, event.Reason)
			}
		case *room.Reply_Room:
			event := payload.Room
			info := RoomInfo{
				Sid:  event.Sid,
				Name: event.Name,
				Lock: event.Lock,
			}
			log.Info("room.RoomInfo: %+v", info)
			if c.OnRoomInfo != nil {
				c.OnRoomInfo(info)
			}

		// case *room.Message:
		// event := payload.Message
		// if c.OnStreamEvent != nil {
		// var roomSignalClients []*Stream
		// for _, st := range event.Streams {
		// roomSignalClient := &Stream{
		// Id: st.Id,
		// }
		// for _, t := range st.Tracks {
		// track := &Track{
		// Id:        t.Id,
		// Label:     t.Label,
		// Kind:      t.Kind,
		// Simulcast: t.Simulcast,
		// }
		// roomSignalClient.Tracks = append(roomSignalClient.Tracks, track)
		// }
		// roomSignalClients = append(roomSignalClients, roomSignalClient)
		// }
		// c.OnStreamEvent(StreamState(event.State), event.Sid, event.Uid, roomSignalClients)
		// }
		default:
			break
		}
	}
}