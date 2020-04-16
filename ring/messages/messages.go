package messages

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"reflect"
	"sync"
	"time"

	"../receivers"

	"../peers"
)

// Enums
const (
	Broadcast = iota
	Ping
	PingAck
)

const MessageLifeTime = 10

type Message struct {
	Purpose        string
	Type           int    // Broadcast or Ping or PingAck
	SenderHostname string // Only necessary for Broadcast (we need to know where it started...)
	Data           []byte
	ExpiresAt      time.Time
}

// Variables
var gInnPort string
var localHostname string
var gBacklog []Message
var gBacklogMutex sync.Mutex

//Public channels
var DisconnectedFromServerChannel = make(chan string)

// Channels
var gServerHostnameChannel = make(chan string)
var gConnectedToServerChannel = make(chan string)

// TODO: Make these channel names more meaningful
var gSendForwardChannel = make(chan Message, 100)
var gSendBackwardChannel = make(chan Message, 100)

func Init(innPort string) {
	gInnPort = innPort
	localHostname = peers.GetRelativeTo(peers.Self, 0)
	go client()
	go server()
	go backlogWatcher()
}

func ConnectTo(hostname string) error {
	if peers.IsAlone() {
		return nil
	}
	gServerHostnameChannel <- hostname
	select {
	case <-gConnectedToServerChannel:
		return nil
	case <-time.After(2 * time.Second):
		return errors.New("TIMED_OUT")
	}
}

// SendMessage takes a byte array and sends it
// to the node which it is connected to by ConnectTo
// purpose is used to filter the message on the receiving end
func SendMessage(purpose string, data []byte) bool {
	if peers.IsAlone() {
		return false
	}
	expire := time.Now().Local().Add(
		time.Second * time.Duration(MessageLifeTime))
	message := Message{
		Purpose:        purpose,
		Type:           Broadcast,
		SenderHostname: localHostname,
		Data:           data,
		ExpiresAt:      expire,
	}
	addToBacklog(message)
	gSendForwardChannel <- message
	return true
}

func backlogWatcher() { // Yaousa!
	for {
		select {
		case <-time.After(500 * time.Millisecond):
			gBacklogMutex.Lock()
			fmt.Println(len(gBacklog))
			for i, msg := range gBacklog {
				if msg.ExpiresAt.Before(time.Now()) {
					copy(gBacklog[i:], gBacklog[i+1:])    // Shift gBacklog[i+1:] left one index.
					gBacklog[len(gBacklog)-1] = Message{} // Erase last element (write zero value).
					gBacklog = gBacklog[:len(gBacklog)-1] // Truncate slice.
					DisconnectedFromServerChannel <- peers.GetNextPeer()
					break
				} else {
					gSendForwardChannel <- msg
				}
			}
			gBacklogMutex.Unlock()
		}
	}
}

func addToBacklog(msg Message) {
	gBacklogMutex.Lock()
	defer gBacklogMutex.Unlock()
	gBacklog = append(gBacklog, msg)
}

func removeFromBacklog(removeMsg Message) {
	gBacklogMutex.Lock()
	defer gBacklogMutex.Unlock()
	for i, msg := range gBacklog {
		if reflect.DeepEqual(msg, removeMsg) {
			copy(gBacklog[i:], gBacklog[i+1:])    // Shift gBacklog[i+1:] left one index.
			gBacklog[len(gBacklog)-1] = Message{} // Erase last element (write zero value).
			gBacklog = gBacklog[:len(gBacklog)-1] // Truncate slice.
			break
		}
	}
}

func GetReceiver(purpose string) chan []byte {
	return receivers.GetChannel(purpose)
}

func client() {
	serverHostname := <-gServerHostnameChannel
	var shouldDisconnectChannel = make(chan bool, 10)
	go handleOutboundConnection(serverHostname, shouldDisconnectChannel)

	// We only want one active client at all times:
	for {
		serverHostname := <-gServerHostnameChannel
		shouldDisconnectChannel <- true
		shouldDisconnectChannel = make(chan bool, 10)
		go handleOutboundConnection(serverHostname, shouldDisconnectChannel)
	}
}

func handleOutboundConnection(server string, shouldDisconnectChannel chan bool) {
	tcpAddr, _ := net.ResolveTCPAddr("tcp4", server)
	conn, err := net.DialTCP("tcp", nil, tcpAddr)
	if err != nil {
		fmt.Printf("TCP client connect error: %s", err)
		return
	}

	defer func() {
		conn.Close()
		if err != nil {
			DisconnectedFromServerChannel <- server
		}
	}()

	gConnectedToServerChannel <- server

	shouldSendPingTicker := time.NewTicker(5 * time.Second)

	pingAckReceivedChannel := make(chan Message, 100)
	connErrorChannel := make(chan error)

	// Read new messages from conn and send them on pingAckReceivedChannel.
	// Errors are sent back on connErrorChannel.
	go receiveMessages(conn, pingAckReceivedChannel, connErrorChannel)

	// Send messages that are passed to gSendForwardChannel
	// Errors are sent back on connErrorChannel.
	go sendMessages(conn, gSendForwardChannel, connErrorChannel)

	for {
		select {
		case <-shouldSendPingTicker.C:
			// Send a ping message at regular intervals to check that
			// the connection is still alive
			messageToSend := Message{
				Type: Ping,
			}
			gSendForwardChannel <- messageToSend
		// select { // Remove?
		case <-pingAckReceivedChannel:
			// We received a PingAck, so everything works fine
			break
			// case <-time.After(1 * time.Second):
			// 	// Cannot retrieve PingAck, so the connection is
			// 	// not working properly

			// 	// err = errors.New("ERR_SERVER_DISCONNECTED")
			// 	// return
			// }
			// break

		case <-shouldDisconnectChannel:
			fmt.Printf("Disconnecting from: %s\n", server)
			return

		case <-connErrorChannel:
			err = errors.New("ERR_SERVER_DISCONNECTED")
			return
		}

	}
}

func server() {
	// Boot up TCP server
	listener, err := net.Listen("tcp", fmt.Sprintf(":%s", gInnPort))
	if err != nil {
		fmt.Printf("TCP server listener error: %s", err) // TODO: Maybe do something with this error
	}

	// Listen to incoming connections
	var shouldDisconnectChannel = make(chan bool, 10)
	for {
		// Accept a new connection
		conn, err := listener.Accept()
		if err != nil {
			fmt.Printf("TCP server accept error: %s", err) // TODO: and this one
			break
		}
		// A new client connected to us, so disconnect to the one
		// already connected because we only accept one connection
		// at all times
		shouldDisconnectChannel <- true
		shouldDisconnectChannel = make(chan bool, 10)
		handleIncomingConnection(conn, shouldDisconnectChannel)

	}
}

func handleIncomingConnection(conn net.Conn, shouldDisconnectChannel chan bool) {
	defer conn.Close()

	messageReceivedChannel := make(chan Message, 100)
	connErrorChannel := make(chan error)

	// Read new messages from conn and send them on pingAckReceivedChannel.
	// Errors are sent back on connErrorChannel.
	go receiveMessages(conn, messageReceivedChannel, connErrorChannel)

	// Send messages that are passed to gSendForwardChannel
	// Errors are sent back on connErrorChannel.
	go sendMessages(conn, gSendBackwardChannel, connErrorChannel)

	for {
		select {
		// We have received a message
		case messageReceived := <-messageReceivedChannel:

			switch messageReceived.Type {
			case Broadcast:
				localHostname := peers.GetRelativeTo(peers.Self, 0)

				if messageReceived.SenderHostname != localHostname {
					// We should forward the message to next node
					receivers.GetChannel(messageReceived.Purpose) <- messageReceived.Data
					gSendForwardChannel <- messageReceived
				} else {
					removeFromBacklog(messageReceived)
				}

				break
			case Ping: // Remove?
				messageToSend := Message{
					Type: PingAck,
				}
				gSendBackwardChannel <- messageToSend
				break
			}
			break

		case <-shouldDisconnectChannel:
			return

		case <-connErrorChannel:
			return
		}

	}
}

func receiveMessages(conn net.Conn, receiveChannel chan Message, errorChannel chan error) {
	for {
		bytesReceived, err := bufio.NewReader(conn).ReadBytes('\n')
		if err != nil {
			errorChannel <- err
			return
		}

		var messageReceived Message
		json.Unmarshal(bytesReceived, &messageReceived)

		receiveChannel <- messageReceived

	}
}

func sendMessages(conn net.Conn, messageToSendChannel chan Message, errorChannel chan error) {
	for {
		messageToSend := <-messageToSendChannel
		serializedMessage, _ := json.Marshal(messageToSend)

		_, err := fmt.Fprintf(conn, string(serializedMessage)+"\n\000")

		if err != nil {
			errorChannel <- err
			return
		}
	}
}
