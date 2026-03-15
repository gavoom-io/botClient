package main

import (
	"encoding/json"
	"log"
	"net/url"

	"github.com/gorilla/websocket"
	"github.com/pion/mediadevices"
	_ "github.com/pion/mediadevices/pkg/driver/camera" // camera driver
	"github.com/pion/mediadevices/pkg/prop"
	"github.com/pion/webrtc/v4"
	"github.com/pion/mediadevices/pkg/codec/x264"
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

func must (err error) {
	if err != nil {
		panic(err)
	}
}

type SDPPayload struct {
    UserID string `json:"userId"`
    SDP    struct {
        Type string `json:"type"`
        SDP  string `json:"sdp"`
    } `json:"sdp"`
}



func handleRequestOffer(pc *webrtc.PeerConnection, conn *websocket.Conn, data json.RawMessage,BotId string) {
log.Println("Raw Offer data:", string(data))    

    var payload SDPPayload
    

    if err := json.Unmarshal(data, &payload); err != nil {
        log.Println("failed to unmarshal sdp offer from client")
        return
    }

    offerType := webrtc.NewSDPType(payload.SDP.Type)

    offer := webrtc.SessionDescription{
        Type: offerType,
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

    log.Println("Created device answer for user", payload.UserID)

    log.Println("received requestOffer for user", payload.SDP)

    conn.WriteJSON(Message{
        Event: "deviceAnswer",
        Data: WebRTCOfferData{
            AuthMessage: AuthMessage{Type: "bot"},
            UserId: payload.UserID,
            BotId:BotId, 
            SDP: *pc.LocalDescription(),
        },
    })
}


func main() {
	// --- Connect WebSocket ---
	u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		log.Fatal("WebSocket dial:", err)
	}
	defer conn.Close()

	// --- Register bot ---
	auth := &BotAuth{
		AuthMessage: AuthMessage{Type: "bot"},
		Password:    "secret",
		Name:        "test",
		ID:          "09eb14ea-6b5f-42ce-b5ca-83e24ef5a828",
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
	
	x264Params, err := x264.NewParams()
	must(err)
	x264Params.Preset = x264.PresetMedium
	x264Params.BitRate = 500_000
	
	codecSelector := mediadevices.NewCodecSelector(
	mediadevices.WithVideoEncoders(&x264Params),
	)
	
	stream, err := mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(640)
			c.Height = prop.Int(480)
			c.FrameRate = prop.Float(30)
		},
		Codec: codecSelector,
	})
	
	must(err)
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
		case "requestOffer":
			handleRequestOffer(pc,conn,ws.Data,auth.ID)
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
