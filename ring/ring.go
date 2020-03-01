package ring

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"strings"
	"syscall"
	"time"

	"./messages"
	"./peers"
)

const (
	NodeChange  = "NodeChange"
	StateChange = "State"
	Call        = "Call"
)

const gBCASTPORT = 6971
const gBroadcastIP = "255.255.255.255"
const gConnectAttempts = 5
const gTIMEOUT = 2
const gJOINMESSAGE = "JOIN"

var isInitialized = false

// Initializes the network if it's present. Establishes a new network if not
func Init() {
	if isInitialized {
		return
	}
	isInitialized = true
	messages.Start()
	go neighbourWatcher()
	go handleRingChange()
	sendJoinMSG() // first send join msg
	/*go*/ handleJoin()
}

//////////////////////////////////////////////
/// Exposed functions for sending and reciving
//////////////////////////////////////////////

func SendMessage(purpose string, data []byte) bool {
	Init()
	return messages.SendMessage(purpose, data)
}

func Recive(purpose string) []byte {
	Init()
	return messages.Receive(purpose)
}
func SendDM(purpose string, ip string, data []byte) bool {
	dataMap := make(map[string][]byte)
	dataMap[ip] = data
	dataMapbytes, _ := json.Marshal(dataMap)
	return messages.SendMessage(purpose, dataMapbytes)
}

func ReciveDM(purpose string) []byte {
	Init()
	for {
		dataMap := make(map[string][]byte)
		dataMapbytes := messages.Receive(purpose)

		json.Unmarshal(dataMapbytes, &dataMap)
		selfIP := peers.GetSelf()
		data, found := dataMap[selfIP]
		if found {
			return data
		}
	}
}

/////////////////////////////////////////////////
/// Functions for setting up and maintaining ring
/////////////////////////////////////////////////

// Uses UDP broadcast to notify any existing ring about its presens
func sendJoinMSG() {
	connWrite := dialBroadcastUDP(gBCASTPORT)
	defer connWrite.Close()

	for i := 0; i < gConnectAttempts; i++ {
		selfIP := peers.GetRelativeTo(peers.Self, 0)
		addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", gBroadcastIP, gBCASTPORT))
		connWrite.WriteTo([]byte(gJOINMESSAGE+":"+selfIP), addr)
		time.Sleep(gTIMEOUT * time.Second) // wait for response
		if !peers.IsAlone() {
			return
		}
	}
}

// Only runs if you are HEAD, listen for new machines broadcasting
// on the network using UDP. The new machine is added to the list of
// known machines. That list is propagted trpough the ring to update the ring
func handleJoin() {
	readChn := make(chan string)
	go blockingRead(readChn)
	for {
		if peers.IsHead() {
			select {
			case tail := <-readChn:
				peers.AddTail(tail)
				nodes, _ := json.Marshal(peers.GetAll())
				if peers.NextIsTail() {
					messages.ConnectTo(tail)
				}
				messages.SendMessage(NodeChange, nodes)
				break

			case <-time.After(10 * time.Second): // Listens for new elevators on the network
				if peers.IsAlone() {
					sendJoinMSG()
				}
				break
			}

		}
	}
}

// Detects if the node infront of you disconnects, alerts rest of ring
// That node becomes the master
func neighbourWatcher() {
	for {
		missingIP := messages.ServerDisconnected()
		fmt.Printf("Disconnect : %s\n", missingIP)

		peers.Remove(missingIP)
		peers.BecomeHead()
		nextNode := peers.GetNextNode()
		messages.ConnectTo(nextNode)

		nodeList := peers.GetAll()
		nodes, _ := json.Marshal(nodeList)
		messages.SendMessage(NodeChange, nodes)
	}
}

// Updates the ring if a node is added or removed.
func handleRingChange() {
	var nodesList []string
	for {
		nodes := messages.Receive(NodeChange)
		json.Unmarshal(nodes, &nodesList)
		if !peers.IsEqualTo(nodesList) {
			peers.Set(nodesList)
			nextNode := peers.GetNextNode()
			messages.ConnectTo(nextNode)
			messages.SendMessage(NodeChange, nodes)
		}
	}
}

////////////////////////////////////////////////////
// Helper functions
////////////////////////////////////////////////////

// Tar inn port, returnerer en udpconn til porten.
func dialBroadcastUDP(port int) net.PacketConn {
	s, _ := syscall.Socket(syscall.AF_INET, syscall.SOCK_DGRAM, syscall.IPPROTO_UDP)
	syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
	syscall.SetsockoptInt(s, syscall.SOL_SOCKET, syscall.SO_BROADCAST, 1)
	syscall.Bind(s, &syscall.SockaddrInet4{Port: port})

	f := os.NewFile(uintptr(s), "")
	conn, _ := net.FilePacketConn(f)
	f.Close()

	return conn
}

// Makes it possible to have timeout on udp read
func blockingRead(readChn chan<- string) {
	buffer := make([]byte, 100)
	connRead := dialBroadcastUDP(gBCASTPORT)

	defer connRead.Close()
	for {
		n, _, _ := connRead.ReadFrom(buffer[0:])
		msg := string(buffer[:n])
		splittedMsg := strings.SplitN(msg, ":", 2)
		if splittedMsg[0] == gJOINMESSAGE && splittedMsg[1] != peers.GetRelativeTo(peers.Self, 0) { // Hmmmmmm
			readChn <- splittedMsg[1]
		}
	}
}