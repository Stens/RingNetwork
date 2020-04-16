package main

import (
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
	go listenRoutine()
	for {
		// fmt.Print("Enter text: ")
		select {
		case <-time.After(2 * time.Second):
			text := "Hello from " + innPort
			ring.BroadcastMessage(purpose, []byte(text))
		}
		// text, _ := reader.ReadString('\n')
		// fmt.Println("Sending succesful!")
	}
}

func listenRoutine() {
	receiver := ring.GetReceiver(purpose)
	shouldPrintTicker := time.NewTicker(2 * time.Second)

	for {
		select {
		case msg := <-receiver:
			fmt.Println("Received: " + string(msg))
			break
		case <-shouldPrintTicker.C:
			fmt.Println(peers.GetAll())
		}
	}
}
