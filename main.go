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

var botId = "82065db3-003d-4e08-9e20-136bb089d795"
var peerConnection *webrtc.PeerConnection
var stream  mediadevices.MediaStream

func cleanupPeerConnection() {


 if peerConnection != nil {
        // Stop all senders BEFORE closing the PC
        for _, sender := range peerConnection.GetSenders() {
            if err := sender.Stop(); err != nil {
                log.Println("sender stop error:", err)
            }
        }
    }
         log.Println("cleaning up ")
        if stream != nil {  
         for _, t := range stream.GetVideoTracks() {
    if v, ok := t.(*mediadevices.VideoTrack); ok {
        v.Close()
    }
}
        }
   stream = nil
         log.Println("cleane dup track now cleaning up pc")

	if peerConnection != nil {
         log.Println("log here")
		if err := peerConnection.Close(); err != nil {
			log.Println("pc close error:", err)
		}
		peerConnection = nil
	}


 log.Println("cleaned up")

        

}

func handleRequestOffer( conn *websocket.Conn, data json.RawMessage, botId string ) {
	var payload SDPPayload


	if err := json.Unmarshal(data, &payload); err != nil {

		log.Println("failed to unmarshal sdp offer:", err)
		return
	}

	offer := webrtc.SessionDescription{
		Type: webrtc.NewSDPType(payload.SDP.Type),
		SDP:  payload.SDP.SDP,
	}

	if err := peerConnection.SetRemoteDescription(offer); err != nil {
		log.Println("SetRemoteDescription error:", err)
		return
	}

	answer, err := peerConnection.CreateAnswer(nil)
	if err != nil {
		log.Println("CreateAnswer error:", err)
		return
	}

	if err := peerConnection.SetLocalDescription(answer); err != nil {
		log.Println("SetLocalDescription error:", err)
		return
	}

         peerConnection.OnICECandidate(func(c *webrtc.ICECandidate) {
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

        peerConnection.OnConnectionStateChange(func(state webrtc.PeerConnectionState) {
                log.Println("PC state:", state)
        })

	conn.WriteJSON(Message{
		Event: "deviceAnswer",
		Data: WebRTCOfferData{
			AuthMessage: AuthMessage{Type: "bot"},
			UserId:      payload.UserID,
			BotId:       botId,
			SDP:         *peerConnection.LocalDescription(),
		},
	})
}

func createPeerconnection(conn *websocket.Conn) (*webrtc.PeerConnection, error) {
	vp8Params, err := vpx.NewVP8Params()
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

        peerConnection = pc
	if err != nil {
		return nil, err
	}

        if stream == nil  {
 
          log.Println("here",stream)
	stream, err = mediadevices.GetUserMedia(mediadevices.MediaStreamConstraints{
		Video: func(c *mediadevices.MediaTrackConstraints) {
			c.Width = prop.Int(320)
			c.Height = prop.Int(240)
			c.FrameRate = prop.Float(15)
		},
		Codec: codecSelector,
	})  
        if err !=  nil {
            log.Println("here is the err", err)
         }

          
         log.Println("streamon 168", stream)
	for _, t := range stream.GetVideoTracks() {
         if peerConnection == nil {
           break
         }
         log.Println("stream ID", stream)
         
          log.Println("stream ID", stream)
         _, err := peerConnection.AddTrack(t)
if err != nil {
    return nil, err
}         
        } 
}

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

        log.Println("stopped program")
	return pc, nil
}

func run() error {
	// --- Media setup ---

	// --- WebSocket ---
	u := url.URL{Scheme: "ws", Host: "192.168.0.141:47000", Path: "/ws"}
	conn, _, err := websocket.DefaultDialer.Dial(u.String(), nil)
	if err != nil {
		return err
	}
	defer conn.Close()

	log.Println("websocket connected")

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
                          cleanupPeerConnection()
                          pc,_ := createPeerconnection(conn) 
                          if err != nil {
                             log.Println("could not create peer connection")
                          }
                          peerConnection = pc
                          log.Println("pc", peerConnection)
                          time.Sleep(2 * time.Second)
			  handleRequestOffer( conn, ws.Data, auth.ID)

		case "ice":
                        log.Println("new Ice Cnadidate")
			var msg ICECandidateMessage
			if err := json.Unmarshal(ws.Data, &msg); err != nil {
				log.Println("failed ICE:", err)
				continue
			}
			if err := peerConnection.AddICECandidate(msg.Candidate); err != nil {
				log.Println("AddICECandidate error:", err)
			}

		case "userDisconnected":
			log.Println("user disconnected → closing PC")
			_ = peerConnection.Close()
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

