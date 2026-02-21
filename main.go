package main

import (
	"encoding/json"
	"log"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	"github.com/pion/mediadevices/pkg/codec/mmal"
	_ "github.com/pion/mediadevices/pkg/driver/camera" // camera driver
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
	ID       int    `json:"id"`
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
}

func main() {
	// --- Connect WebSocket ---
	u := url.URL{Scheme: "ws", Host: "localhost:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("WebSocket dial:", err)
	}
	defer conn.Close()

	// --- Register bot ---
	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"},
		Password:    "secret",
		Name:        "device1",
		ID:          3,
		CamID:       "cam123",
		CamName:     "Front Cam",
	}
	conn.WriteJSON(Message{Event: "registerBot", Data: auth})

	// --- WebRTC setup ---
	m := &webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: "video/h264", ClockRate: 90000},
		PayloadType:        96,
	}, webrtc.RTPCodecTypeVideo)

	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Fatal("NewPeerConnection:", err)
	}

	// --- Capture camera stream with H.264 ---
	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(640)
			c.Height = prop.Int(480)
			c.FrameRate = prop.Float(30)
		},
		Codec: mediadevices.NewCodecSelector(
			mediadevices.WithVideoEncoders(&mmal.Params{}),
		),
	})
	if err != nil {
		log.Fatal("GetUserMedia:", err)
	}

	// --- Add video tracks to PeerConnection ---
	for _, t := range stream.GetVideoTracks() {
		_, err := pc.AddTrack(t) // Track already handles H.264 encoding
		if err != nil {
			log.Fatal("AddTrack:", err)
		}
	}

	// --- Data channel ---
	dc, err := pc.CreateDataChannel("commands", nil)
	if err != nil {
		log.Fatal("CreateDataChannel:", err)
	}
	dc.OnMessage(func(msg webrtc.DataChannelMessage) {
		log.Println("Command received:", string(msg.Data))
	})

	// --- Create offer ---
	offer, err := pc.CreateOffer(nil)
	if err != nil {
		log.Fatal("CreateOffer:", err)
	}
	if err := pc.SetLocalDescription(offer); err != nil {
		log.Fatal("SetLocalDescription:", err)
	}

	conn.WriteJSON(Message{
		Event: "webrtcOffer",
		Data: WebRTCOfferData{
			AuthMessage: AuthMessage{Type: "bot"},
			SDP:         offer,
		},
	})

	// --- WebSocket listener ---
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("ws.ReadMessage:", err)
			continue
		}

		var ws WSMessage
		if err := json.Unmarshal(msgBytes, &ws); err != nil {
			log.Println("unmarshal WS message:", err)
			continue
		}

		switch ws.Event {
		case "webrtcAnswer":
			var answer webrtc.SessionDescription
			json.Unmarshal(ws.Data, &answer)
			pc.SetRemoteDescription(answer)
		case "ice":
			var ice webrtc.ICECandidateInit
			json.Unmarshal(ws.Data, &ice)
			pc.AddICECandidate(ice)
		case "control":
			var ctrl map[string]string
			json.Unmarshal(ws.Data, &ctrl)
			log.Println("Control command:", ctrl["command"])
		}
	}
}
