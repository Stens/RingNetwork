package main

import (
	"bufio"
	"fmt"
	"os"

	"./ring"
)

const purpose = "bcast"

func main() {
	ring.Init()
	reader := bufio.NewReader(os.Stdin)
	for {
		fmt.Print("Enter text: ")
		text, _ := reader.ReadString('\n')
		if ring.SendMessage(purpose, []bytes(text)) {
			fmt.Println("Sending succesful!")
		} else {
			fmt.Println("Sending failed")
		}
	}
}

func listenRoutine() {
	for {
		recivedBytes := ring.Recive(purpose)
		fmt.Println(string(recivedBytes))
	}
}
