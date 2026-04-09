package main

import (
	"encoding/json"
	"log"
	"net/url"
         "context"
	"time"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/vpx"
	_ "github.com/pion/mediadevices/pkg/driver/camera"
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v4"
)

type AuthMessage struct {
	Type string `json:"type"`
}

type WSMessage struct {
	Event string          `json:"event"`
	Data  json.RawMessage `json:"data"`
}

type BotAuth struct {
	AuthMessage
	Password string `json:"password"`
	Name     string `json:"name"`
	ID       string `json:"id"`
	CamID    string `json:"camId"`
	CamName  string `json:"camName"`
}

type Message struct {
	Event string      `json:"event"`
	Data  interface{} `json:"data"`
}

type WebRTCOfferData struct {
	AuthMessage AuthMessage               `json:"auth"`
	SDP         webrtc.SessionDescription `json:"sdp"`
	UserId      string                    `json:"userId"`
	BotId       string                    `json:"botId"`
}

type SDPPayload struct {
	UserID string `json:"userId"`
	SDP    struct {
		Type string `json:"type"`
		SDP  string `json:"sdp"`
	} `json:"sdp"`
}

type ICECandidateMessage struct {
	BotID     string                  `json:"botId"`
	UserID    string                  `json:"userId"`
	Candidate webrtc.ICECandidateInit `json:"candidate"`
}

type BotClient struct {
	BotID    string
	PC       *webrtc.PeerConnection
	Stream   mediadevices.MediaStream
	sendChan chan Message
	busy     bool
        restart chan struct {}
        done chan struct {}
        cancelRTC context.CancelFunc  
}

var botId = "82065db3-003d-4e08-9e20-136bb089d795"

func (bc *BotClient) cleanup(iceChan chan ICECandidateMessage) {
	if bc.PC != nil {
		if err := bc.PC.Close(); err != nil {
			log.Println("pc close error:", err)
		}
		bc.PC = nil
	}
	if bc.Stream != nil {
		for _, t := range bc.Stream.GetVideoTracks() {
			t.Close()
			log.Println("stopped video track:", t.ID())
		}
		for _, t := range bc.Stream.GetAudioTracks() {
			t.Close()
			log.Println("stopped audio track:", t.ID())
		}
		bc.Stream = nil
	}
select {
    case bc.restart <- struct{}{}: // send restart signal
        log.Println("🔄 RTC restart requested")
    default:
        // do nothing if already signaled (prevents blocking)
    }

}

func (bc *BotClient) handleRequestOffer(data json.RawMessage, iceChan chan ICECandidateMessage) {
	ctx, cancel := context.WithCancel(context.Background())
	bc.cancelRTC = cancel

	vp8Params, err := vpx.NewVP8Params()
	if err != nil {
		log.Println("vp8 params error:", err)
		cancel()
		return
	}
	vp8Params.BitRate = 500_000

	codecSelector := mediadevices.NewCodecSelector(
		mediadevices.WithVideoEncoders(&vp8Params),
	)

	m := &webrtc.MediaEngine{}
	codecSelector.Populate(m)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	pc, err := api.NewPeerConnection(webrtc.Configuration{
		ICEServers: []webrtc.ICEServer{
			{URLs: []string{"stun:stun.l.google.com:19302"}},
		},
	})
	if err != nil {
		log.Println("NewPeerConnection error:", err)
		cancel()
		return
	}

	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(320)
			c.Height = prop.Int(240)
			c.FrameRate = prop.Float(15)
		},
		Codec: codecSelector,
	})
	if err != nil {
		log.Println("GetUserMedia error:", err)
		cancel()
		return
	}

	for _, t := range stream.GetVideoTracks() {
		if _, err := pc.AddTrack(t); err != nil {
			log.Println("AddTrack error:", err)
			cancel()
			return
		}
	}

	bc.PC = pc
	bc.Stream = stream

	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		select {
		case <-ctx.Done():
			log.Println("ICE candidate dropped, context cancelled")
			return
		default:
		}
		msg := ICECandidateMessage{
			BotID:     bc.BotID,
			Candidate: c.ToJSON(),
		}
		b, _ := json.Marshal(msg)
		select {
		case bc.sendChan <- Message{Event: "iceCandidate", Data: json.RawMessage(b)}:
		case <-ctx.Done():
			log.Println("ICE candidate dropped on send, context cancelled")
		}
	})

	pc.OnICEConnectionStateChange(func(state webrtc.ICEConnectionState) {
		log.Println("ICE state:", state)
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("PC state:", state)
		switch state {
		case webrtc.PeerConnectionStateConnecting:
			go func() {
				time.Sleep(3 * time.Second)
				if bc.PC != nil && bc.PC.ConnectionState() == webrtc.PeerConnectionStateConnecting {
					log.Println("PC stuck in connecting after 3s, closing...")
					bc.PC.Close()
				}
			}()
		case webrtc.PeerConnectionStateFailed:
			log.Println("PC failed, closing connection...")
			cancel() // cancel context before any restart
			select {
			case bc.restart <- struct{}{}:
				log.Println("💣 RTC failed → requesting restart")
			default:
			}
		case webrtc.PeerConnectionStateDisconnected:
			log.Println("PC disconnected, closing...")
			bc.PC.Close()
		case webrtc.PeerConnectionStateClosed:
			log.Println("connection closed")
			bc.busy = false
		case webrtc.PeerConnectionStateConnected:
			log.Println("state connected")
			bc.busy = false
		}
	})

	var payload SDPPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Println("failed to unmarshal sdp offer:", err)
		cancel()
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.NewSDPType(payload.SDP.Type),
		SDP:  payload.SDP.SDP,
	}
	if err := pc.SetRemoteDescription(offer); err != nil {
		log.Println("SetRemoteDescription error:", err)
		cancel()
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Println("CreateAnswer error:", err)
		cancel()
		return
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		log.Println("SetLocalDescription error:", err)
		cancel()
		return
	}

	// ICE drain goroutine — exits when iceChan is closed or ctx is cancelled
	go func() {
		for {
			select {
			case <-ctx.Done():
				log.Println("ICE drain goroutine exiting")
				return
			case msg, ok := <-iceChan:
				if !ok {
					log.Println("iceChan closed, ICE drain exiting")
					return
				}
				if err := pc.AddICECandidate(msg.Candidate); err != nil {
					log.Println("AddICECandidate error:", err)
				}
			}
		}
	}()

	select {
	case bc.sendChan <- Message{
		Event: "deviceAnswer",
		Data: WebRTCOfferData{
			AuthMessage: AuthMessage{Type: "bot"},
			UserId:      payload.UserID,
			BotId:       bc.BotID,
			SDP:         *pc.LocalDescription(),
		},
	}:
	case <-ctx.Done():
		log.Println("deviceAnswer dropped, context cancelled")
	}
}

func (bc *BotClient) run() error {

        if bc.cancelRTC != nil {
        bc.cancelRTC()
    }
	bc.busy = false
	iceChan := make(chan ICECandidateMessage, 20)
	bc.sendChan = make(chan Message, 32)

	u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Println("should disconnect now")
		return err
	}

	defer func() {
                if bc.cancelRTC != nil {
                bc.cancelRTC()
                 }
                time.Sleep(50 * time.Millisecond)
		conn.Close()
                bc.cleanup(iceChan)
		 for len(bc.sendChan) > 0 {
                 <-bc.sendChan
                  }
		close(iceChan)
		log.Println("WebSocket disconnected")
	}()

	log.Println("websocket connected")

	go func() {
		for msg := range bc.sendChan {
			if err := conn.WriteJSON(msg); err != nil {
				log.Println("write error:", err)
				return
			}
		}
	}()

	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"},
		Password:    "secret",
		Name:        "test",
		ID:          bc.BotID,
		CamID:       "cam123",
		CamName:     "Front Cam",
	}

	bc.sendChan <- Message{Event: "registerBot", Data: auth}

	for {

		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("ws.ReadMessage:", err)
			return err
		}

		var ws WSMessage
		if err := json.Unmarshal(msgBytes, &ws); err != nil {
			log.Println("unmarshal WS message:", err)
			continue
		}

		switch ws.Event {

		case "requestOffer":
                        
                         if bc.busy {
                           log.Println("connection in progress chill out")
                            continue
                         }
                         bc.busy = true
                         oldIceChan := iceChan 
			 // kills old drain goroutine cleanly
			iceChan = make(chan ICECandidateMessage, 20)
                         bc.cleanup(oldIceChan)
			go bc.handleRequestOffer(ws.Data, iceChan)

		case "ice":
			var msg ICECandidateMessage
			if err := json.Unmarshal(ws.Data, &msg); err != nil {
				log.Println("failed ICE:", err)
				continue
			}
			iceChan <- msg

		case "userDisconnected":
			log.Println("user disconnected → closing PC")
                        conn.Close()
			return nil
		}
	
log.Println("run exited")
}}

func main() {
	bc := &BotClient{
		BotID: botId,
                restart: make(chan struct{}),
	}

	for {
		log.Println("starting session...")
		err := bc.run()
		log.Println("session ended:", err)
		time.Sleep(2 * time.Second)
	}
}
