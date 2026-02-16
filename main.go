package main

import (
	"encoding/json"
	"log"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/pion/webrtc/v3"
)

type AuthMessage struct {
	Type string `json:"type"`
}

type WSMessage struct {
	Type    string          `json:"type"`
	Payload json.RawMessage `json:"payload"`
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

func main() {
	u := url.URL{Scheme: "ws", Host: "localhost:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal(err)
	}
	defer conn.Close()

	// Send register event
	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"}, // must match embedded field name
		Password:    "secret",
		Name:        "device1",
		ID:          3,
		CamID:       "cam123",
		CamName:     "Front Cam",
	}

	message := Message{
		Event: "registerBot",
		Data:  auth,
	}
	conn.WriteJSON(message)

	m := &webrtc.MediaEngine{}
	m.RegisterCodec(webrtc.RTPCodecParameters{
		RTPCodecCapability: webrtc.RTPCodecCapability{MimeType: "video/h264", ClockRate: 90000},
		PayloadType:        102,
	}, webrtc.RTPCodecTypeVideo)
	api := webrtc.NewAPI(webrtc.WithMediaEngine(m))

	// Create PeerConnection
	pc, err := api.NewPeerConnection(webrtc.Configuration{})
	if err != nil {
		log.Fatal(err)
	}

	// Create H264 video track
	videoTrack, err := webrtc.NewTrackLocalStaticSample(
		webrtc.RTPCodecCapability{MimeType: "video/h264"}, "video", "pion-h264",
	)
	if err != nil {
		log.Fatal(err)
	}

	_, err = pc.AddTrack(videoTrack)
	if err != nil {
		log.Fatal(err)
	}

	offer, _ := pc.CreateOffer(nil)
	pc.SetLocalDescription(offer)

	offerMessage := Message{
		Event: "webrtcOffer",
		Data:  offer,
	}

	conn.WriteJSON(offerMessage)

	// Periodic telemetry
	for {
		_, msgBytes, err := conn.ReadMessage()
		if err != nil {
			log.Println("ReadMessage error:", err)
		}
		var msg WSMessage
		json.Unmarshal(msgBytes, &msg)

		switch msg.Type {
		case "answer":
			var answer webrtc.SessionDescription
			json.Unmarshal(msg.Payload, &answer)
			pc.SetRemoteDescription(answer)

		case "ice":
			var ice webrtc.ICECandidateInit
			json.Unmarshal(msg.Payload, &ice)
			pc.AddICECandidate(ice)

		case "control":
			var ctrl map[string]string
			json.Unmarshal(msg.Payload, &ctrl)
			log.Println("Received control:", ctrl["command"])
		}
	}
}
