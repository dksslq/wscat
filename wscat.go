package main

import "time"
import "syscall"
import "fmt"
import "os"
import "os/signal"
import "flag"
import "net"

import go_log "log"
import "github.com/gorilla/websocket"

var Verbose bool
var Proto string
var Addr string
var Path string
var MsgType string

var URL string

func log(v ...interface{}) {
	if !Verbose {
		return
	}
	go_log.Println(v...)
}

func init() {
	flag.BoolVar(&Verbose, "v", false, "verbose output")
	flag.StringVar(&Proto, "proto", "ws", "ws(plain text) or wss(ws over tls)")
	flag.StringVar(&Addr, "addr", "", "addr to connect")
	flag.StringVar(&Path, "path", "/", "websocket url path")
	flag.StringVar(&MsgType, "type", "text", "websocket message type")
	flag.Parse()

	switch Proto {
	case "ws":
	case "wss":
	default:
		go_log.Fatal("Proto", Proto, "is not supported.")
	}

	if Path[0] != '/' {
		go_log.Fatal("Path must start with `/'")
	}

	if Addr == "" {
		go_log.Fatal("Must specify addr to connect to.")
	}

	host, port, err := net.SplitHostPort(Addr)
	if err != nil {
		go_log.Fatal(err)
	}

	switch MsgType {
	case "text":
	case "bin":
	default:
		go_log.Fatal("MsgType", MsgType, "is not supported.")
	}

	URL = Proto + "://" + host
	if (Proto == "ws" && port != "80") || (Proto == "wss" && port != "443") {
		URL += ":" + port
	}
	URL += Path

}

func main() {
	log("Dialing to", URL)
	dialer := &websocket.Dialer{
		ReadBufferSize:   4 * 1024,
		WriteBufferSize:  4 * 1024,
		HandshakeTimeout: time.Second * 8,
	}

	conn, _, err := dialer.Dial(URL, map[string][]string{})
	if err != nil {
		go_log.Fatal(err)
	}
	log("Connected")

	// i
	statistics_i := make(chan int64, 0)
	go func() {
		var count int64
		for {
			_, msg, err := conn.ReadMessage()
			count += int64(len(msg))
			if len(msg) > 0 {
				os.Stdout.Write(msg)
			}
			if err != nil {
				log(err)
				break
			}
		}

		statistics_i <- count
	}()

	// o
	statistics_o := make(chan int64, 0)
	go func() {
		var count int64
		buf := make([]byte, 32768)
		for {
			ln, err := os.Stdin.Read(buf)
			var ew error
			if ln > 0 {
				msgtype := websocket.TextMessage
				if MsgType != "text" {
					msgtype = websocket.BinaryMessage
				}
				ew = conn.WriteMessage(msgtype, buf[:ln])
			}
			if err != nil || ew != nil {
				switch error(nil) {
				case err:
					log(ew)
				case ew:
					log(err)
				}
				break
			}
			count += int64(ln)

		}

		statistics_o <- count
	}()

	// keyboard signal
	go func() {
		sigc := make(chan os.Signal)
		signal.Notify(sigc, syscall.SIGHUP, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)
		sig := <-sigc
		log("signal:", sig, fmt.Sprintf("(0x%02x)", int(sig.(syscall.Signal))))
		conn.Close()
	}()

	// wait worker
	log("statistics:", <-statistics_i, "bytes recv,", <-statistics_o, "bytes sent")
}
