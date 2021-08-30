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

	if err != nil {
		log.Errorf("error: %v", err)
		return nil
	}
	go c.roomSignalReadLoop()
	return c
}

// CreateRoom create a room
func (c *RoomClient) CreateRoom(ctx context.Context, req *room.CreateRoomRequest) (*room.CreateRoomReply, error) {
	return c.roomServiceClient.CreateRoom(c.ctx, req)
}

// UpdateRoom lock/unlock a room, avoid someone join
func (c *RoomClient) UpdateRoom(ctx context.Context, req *room.UpdateRoomRequest) (*room.UpdateRoomReply, error) {
	return c.roomServiceClient.UpdateRoom(c.ctx, req)
}

// EndRoom delete a room
func (c *RoomClient) EndRoom(ctx context.Context, req *room.EndRoomRequest) (*room.EndRoomReply, error) {
	return c.roomServiceClient.EndRoom(c.ctx, req)
}

// GetRooms get all rooms
func (c *RoomClient) GetRooms(ctx context.Context, req *room.GetRoomsRequest) (*room.GetRoomsReply, error) {
	return c.roomServiceClient.GetRooms(c.ctx, req)
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

// EditPeerInfo ..
func (c *RoomClient) UpdatePeer(ctx context.Context, req *room.UpdatePeerRequest) (*room.UpdatePeerReply, error) {
	return c.roomServiceClient.UpdatePeer(c.ctx, req)
}

func (c *RoomClient) Close() {
	c.cancel()
	c.roomSignalStream.CloseSend()
	log.Infof("Close ok")
}

func (c *RoomClient) Join(j JoinInfo) error {
	log.Infof("join=%+v", j)

	if j.Sid == "" {
		log.Errorf("invalid sid [%v]", j.Sid)
		c.OnError(ErrorInvalidParams)
		return ErrorInvalidParams
	}
	if j.Uid == "" {
		j.Uid = RandomKey(6)
	}
	err := c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_Join{
				Join: &room.JoinRequest{
					Peer: &room.Peer{
						Sid:         j.Sid,
						Uid:         j.Uid,
						DisplayName: j.DisplayName,
						ExtraInfo:   j.ExtraInfo,
						Destination: j.Destination,
						Role:        room.Role(j.Role),
						Protocol:    room.Protocol(j.Protocol),
						Avatar:      j.Avatar,
						Direction:   room.Peer_Direction(j.Direction),
						Vendor:      j.Vendor,
					},
				},
			},
		},
	)
	if err != nil {
		log.Errorf("[%v] err=%v", j.Sid, err)
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
// from is a uid
// to is a uid or "all"
func (c *RoomClient) SendMessage(sid, from, to string, data map[string]interface{}) error {
	log.Infof("from=%v, to=%v, data=%v", from, to, data)
	buf, err := json.Marshal(data)
	if err != nil {
		log.Errorf("Marshal msg.data [%v] err=%v", from, err)
		c.OnError(err)
		return err
	}
	err = c.roomSignalStream.Send(
		&room.Request{
			Payload: &room.Request_SendMessage{
				SendMessage: &room.SendMessageRequest{
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
		log.Errorf("error: %v", err)
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

			if c.OnPeerEvent != nil {
				c.OnPeerEvent(
					PeerState(event.State),
					PeerInfo{
						Sid:         event.Peer.Sid,
						Uid:         event.Peer.Uid,
						DisplayName: event.Peer.DisplayName,
						ExtraInfo:   event.Peer.ExtraInfo,
						Destination: event.Peer.Destination,
						Role:        Role(event.Peer.Role),
						Protocol:    Protocol(event.Peer.Protocol),
						Avatar:      event.Peer.Avatar,
						Direction:   Peer_Direction(event.Peer.Direction),
						Vendor:      event.Peer.Vendor,
					},
				)
			}
		case *room.Reply_Disconnect:
			event := payload.Disconnect
			log.Infof("room.Disconnect: %+v", event)
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
			log.Infof("room.RoomInfo: %+v", info)
			if c.OnRoomInfo != nil {
				c.OnRoomInfo(info)
			}
		default:
			break
		}
	}
}
