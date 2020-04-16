# RingNetwork
A simple ring network written in Go. The nodes on the network find eachother using UDP and then connects and communicate over TCP.
Used for communicating between N number of elevators for the course TTK4145 at NTNU.

Simulate packet loss:

    sudo iptables -A INPUT -p tcp --[s-d]port [port] -m statistic --mode random --probability 0.2 -j DROP
