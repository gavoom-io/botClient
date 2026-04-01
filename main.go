package main

import (
	"encoding/json"
	"log"
	"net/url"
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

var botId = "09eb14ea-6b5f-42ce-b5ca-83e24ef5a828"

func cleanupPeerConnection(pc *webrtc.PeerConnection, stream mediadevices.MediaStream) {
    if pc != nil {
        if err := pc.Close(); err != nil {
            log.Println("pc close error:", err)
        }
        pc = nil
    }

    if stream != nil {
        for _, t := range stream.GetVideoTracks() {
            t.Close() // releases the camera
            log.Println("stopped video track:", t.ID())
        }
        for _, t := range stream.GetAudioTracks() {
            t.Close()
            log.Println("stopped audio track:", t.ID())
        }
        stream = nil
    }
}

func handleRequestOffer(pc *webrtc.PeerConnection, conn *websocket.Conn, data json.RawMessage, botId string) {
	var payload SDPPayload
	if err := json.Unmarshal(data, &payload); err != nil {
		log.Println("failed to unmarshal sdp offer:", err)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.NewSDPType(payload.SDP.Type),
		SDP:  payload.SDP.SDP,
	}

	if err := pc.SetRemoteDescription(offer); err != nil {
		log.Println("SetRemoteDescription error:", err)
		return
	}

	answer, err := pc.CreateAnswer(nil)
	if err != nil {
		log.Println("CreateAnswer error:", err)
		return
	}

	if err := pc.SetLocalDescription(answer); err != nil {
		log.Println("SetLocalDescription error:", err)
		return
	}

	conn.WriteJSON(Message{
		Event: "deviceAnswer",
		Data: WebRTCOfferData{
			AuthMessage: AuthMessage{Type: "bot"},
			UserId:      payload.UserID,
			BotId:       botId,
			SDP:         *pc.LocalDescription(),
		},
	})
}

func run() error {
	// --- Media setup ---
	vp8Params, err := vpx.NewVP8Params()
	if err != nil {
		return err
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
		return err
	}
	 
        var stream mediadevices.MediaStream

	stream, err = mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(320)
			c.Height = prop.Int(240)
			c.FrameRate = prop.Float(15)
		},
		Codec: codecSelector,
	})

        defer cleanupPeerConnection(pc,stream)
	if err != nil {
		return err
	}

	for _, t := range stream.GetVideoTracks() {
		if _, err := pc.AddTrack(t); err != nil {
			return err
		}
	}

	// --- WebSocket ---
	u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Println("websocket connected")

	// ICE handler
	pc.OnICECandidate(func(c *webrtc.ICECandidate) {
		if c == nil {
			return
		}
		msg := ICECandidateMessage{
			BotID:     botId,
			Candidate: c.ToJSON(),
		}
		b, _ := json.Marshal(msg)
		conn.WriteJSON(WSMessage{Event: "iceCandidate", Data: b})
	})

	pc.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
		log.Println("PC state:", state)
	})

	// register bot
	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"},
		Password:    "secret",
		Name:        "test",
		ID:          botId,
		CamID:       "cam123",
		CamName:     "Front Cam",
	}

	conn.WriteJSON(Message{Event: "registerBot", Data: auth})

	// --- Read loop ---
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("ws.ReadMessage:", err)
			return err // ✅ triggers reconnect
		}

		var ws WSMessage
		if err := json.Unmarshal(msgBytes, &ws); err != nil {
			log.Println("unmarshal WS message:", err)
			continue
		}

		switch ws.Event {

		case "requestOffer":
			handleRequestOffer(pc, conn, ws.Data, auth.ID)

		case "ice":
			var msg ICECandidateMessage
			if err := json.Unmarshal(ws.Data, &msg); err != nil {
				log.Println("failed ICE:", err)
				continue
			}
			if err := pc.AddICECandidate(msg.Candidate); err != nil {
				log.Println("AddICECandidate error:", err)
			}

		case "userDisconnected":
			log.Println("user disconnected → closing PC")
			return nil // restart session cleanly
		}
	}
}

func main() {
	for {
		log.Println("starting session...")

		err := run()

		log.Println("session ended:", err)

		time.Sleep(2 * time.Second)
	}
}
