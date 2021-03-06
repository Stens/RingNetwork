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

const gBCASTPORT = 6971
const gBroadcastIP = "255.255.255.255"
const gConnectAttempts = 5
const gTIMEOUT = 2
const gJOINMESSAGE = "JOIN"
const NodeChange = "NodeChange"

// Initializes the network if it's present. Establishes a new network if not
func Init(innPort string) error {
	peersError := peers.Init(innPort)
	fmt.Println("Started peers server")
	if peersError != nil {
		fmt.Println("Error starting peers server")
		fmt.Println(peersError)
		return peersError
	}
	messages.Init(innPort)
	fmt.Println("Started messages")
	go handleJoin(innPort)
	go ringWatcher()
	fmt.Println("Starting ring...")
	return nil
}

//////////////////////////////////////////////
/// Exposed functions for sending and reciving
//////////////////////////////////////////////

func BroadcastMessage(purpose string, data []byte) bool {
	return messages.SendMessage(purpose, data)
}

func GetReceiver(purpose string) chan []byte {
	return messages.GetReceiver(purpose)
}

func SendToPeer(purpose string, ip string, data []byte) bool {
	dataMap := make(map[string][]byte)
	dataMap[ip] = data
	dataMapbytes, _ := json.Marshal(dataMap)
	return messages.SendMessage(purpose, dataMapbytes)
}

//////////////////////////	///////////////////////
/// Functions for setting up and maintaining ring
/////////////////////////////////////////////////

// Only runs if you are HEAD, listen for new machines broadcasting
// on the network using UDP. The new machine is added to the list of
// known machines. That list is propagted trpough the ring to update the ring
func handleJoin(innPort string) {
	readChn := make(chan string)
	go nonBlockingRead(readChn)
	// sendJoinMSG(innPort)
	for {
		select {
		case tail := <-readChn:
			if !peers.IsHead() {
				break
			}
			if !peers.AddTail(tail) {
				break
			}
			if peers.IsNextTail() {
				fmt.Printf("Connecting to: %s\n", tail)
				messages.ConnectTo(tail)
			}
			nodes := peers.GetAll()
			nodesBytes, _ := json.Marshal(nodes)
			messages.SendMessage(NodeChange, nodesBytes)
			break

		case <-time.After(5 * time.Second): // Listens for new elevators on the network
			if peers.IsAlone() {
				sendJoinMSG(innPort)
			}
			break
		}
	}
}

// Handles ring growth and shrinking
// Detects if the node infront of you disconnects, alerts rest of ring
// That node becomes the master
func ringWatcher() {
	var nodesList []string
	var disconnectedIP string

	nodeChangeReciver := messages.GetReceiver(NodeChange)

	for {
		select {
		case disconnectedIP = <-messages.DisconnectedFromServerChannel:
			fmt.Printf("Disconnect : %s\n", disconnectedIP)

			peers.Remove(disconnectedIP)
			if peers.IsAlone() {
				break
			}
			peers.BecomeHead()
			nextNode := peers.GetNextPeer()
			fmt.Printf("Connecting to: %s\n", nextNode)
			messages.ConnectTo(nextNode)

			nodeList := peers.GetAll()
			nodeBytes, _ := json.Marshal(nodeList)
			messages.SendMessage(NodeChange, nodeBytes)
			break
		case nodeBytes := <-nodeChangeReciver:
			json.Unmarshal(nodeBytes, &nodesList)
			if !peers.IsEqualTo(nodesList) {
				peers.Set(nodesList)
				nextNode := peers.GetNextPeer()
				fmt.Printf("Connecting to: %s\n", nextNode)
				messages.ConnectTo(nextNode)
				messages.SendMessage(NodeChange, nodeBytes)
			}
			break
		}
	}
}

// Uses UDP broadcast to notify any existing ring about its presens
func sendJoinMSG(innPort string) {
	connWrite := dialBroadcastUDP(gBCASTPORT)
	defer connWrite.Close()

	for i := 0; i < gConnectAttempts; i++ {
		selfIP := peers.GetRelativeTo(peers.Self, 0)
		addr, _ := net.ResolveUDPAddr("udp4", fmt.Sprintf("%s:%d", gBroadcastIP, gBCASTPORT))
		message := gJOINMESSAGE + "-" + selfIP
		connWrite.WriteTo([]byte(message), addr)
		time.Sleep(gTIMEOUT * time.Second) // wait for response
		if !peers.IsAlone() {
			return
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
func nonBlockingRead(readChn chan<- string) { // This is iffy, was a quick fix
	buffer := make([]byte, 100)
	connRead := dialBroadcastUDP(gBCASTPORT)

	defer connRead.Close()
	for {
		nBytes, _, err := connRead.ReadFrom(buffer[0:]) // Fuck errors
		if err != nil {
			continue
		}
		msg := string(buffer[:nBytes])
		splittedMsg := strings.SplitN(msg, "-", 2)
		self := peers.GetRelativeTo(peers.Self, 0)
		receivedJoin := splittedMsg[0]
		receivedHost := splittedMsg[1]
		if receivedJoin == gJOINMESSAGE && receivedHost != self { // Hmmmmmm
			readChn <- receivedHost
		}
	}
}
