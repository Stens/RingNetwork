package main

import (
	"bufio"
	"fmt"
	"os"
	"time"

	"./ring"
	"./ring/peers"
)

const purpose = "bcast"

func main() {
	innPort := os.Args[1]
	ring.Init(innPort)
	reader := bufio.NewReader(os.Stdin)
	go listenRoutine()
	for {
		// fmt.Print("Enter text: ")
		text, _ := reader.ReadString('\n')
		if ring.BroadcastMessage(purpose, []byte(text)) {
			// fmt.Println("Sending succesful!")
		} else {
			fmt.Println("Sending failed")
		}
	}
}

func listenRoutine() {
	receiver := ring.GetReceiver(purpose)
	for {
		select {
		case msg := <-receiver:
			fmt.Println("Received: " + string(msg))
			break
		case <-time.After(1 * time.Second):
			fmt.Println(peers.GetAll())
		}
	}
}
