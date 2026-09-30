// source-stage-simulator is an independent PETXY/1 device process. It never
// imports the production stage driver and can inject motion faults for testing.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"log"
	"net"
	"strconv"
	"strings"
	"time"
)

func main() {
	address := flag.String("listen", "127.0.0.1:19010", "PETXY/1 TCP listen address")
	delay := flag.Duration("move-delay", 10*time.Millisecond, "delay before motion acknowledgement")
	fault := flag.String("fault", "", "motion fault: disconnect, malformed, timeout")
	flag.Parse()
	listener, err := net.Listen("tcp", *address)
	if err != nil {
		log.Fatal(err)
	}
	log.Printf("Source stage simulator listening on %s", listener.Addr())
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Fatal(err)
		}
		go serve(conn, *delay, *fault)
	}
}

func serve(conn net.Conn, delay time.Duration, fault string) {
	defer conn.Close()
	scanner := bufio.NewScanner(conn)
	scanner.Buffer(make([]byte, 256), 256)
	configured, zeroed := false, false
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) == 0 {
			continue
		}
		response := "ERROR command"
		switch fields[0] {
		case "HELLO":
			response = "PETXY 1"
		case "K":
			continue
		case "CONFIG":
			if len(fields) == 5 {
				configured = true
				response = "CONFIGURED"
			}
		case "ZERO":
			if configured {
				zeroed = true
				response = "ZEROED"
			}
		case "S":
			zeroed = false
			response = "STOPPED"
		case "MOVE":
			if len(fields) != 3 || !zeroed {
				break
			}
			x, xe := strconv.ParseInt(fields[1], 10, 32)
			y, ye := strconv.ParseInt(fields[2], 10, 32)
			if xe != nil || ye != nil {
				break
			}
			switch fault {
			case "disconnect":
				return
			case "timeout":
				continue
			case "malformed":
				response = "READY nonsense"
			default:
				time.Sleep(delay)
				response = fmt.Sprintf("READY %d %d", x, y)
			}
		}
		if _, err := fmt.Fprintln(conn, response); err != nil {
			return
		}
	}
}
