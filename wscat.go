package main

import "context"
import "flag"
import "fmt"
import "net"
import "net/http"
import "os"
import "os/signal"
import "strings"
import "syscall"
import "time"

import damn "log"

import "github.com/gorilla/websocket"

var Verbose bool
var Listen bool
var Proto string
var Bind string
var Addr string
var Path string
var MsgType string

func log(v ...interface{}) {
	if !Verbose {
		return
	}
	damn.Println(v...)
}

func warning(v ...interface{}) {
	damn.Println(v...)
}

func fatal(v ...interface{}) {
	damn.Fatal(v...)
}

func init() {
	flag.BoolVar(&Verbose, "v", false, "verbose output")
	flag.BoolVar(&Listen, "listen", false, "run as websocket server")
	flag.StringVar(&Proto, "proto", "ws", "ws(plain text) or wss(ws over tls)")
	flag.StringVar(&Bind, "bind", "", "local addr")
	flag.StringVar(&Addr, "addr", "", "addr to connect")
	flag.StringVar(&Path, "path", "/", "websocket url path")
	flag.StringVar(&MsgType, "type", "text", "websocket message type")
	flag.Parse()

	switch Proto {
	case "ws":
	case "wss":
	default:
		fatal("Proto", Proto, "is not supported.")
	}

	if Path[0] != '/' {
		fatal("Path must start with `/'")
	}

	if !Listen && Addr == "" {
		fatal("Must specify addr to connect to.")
	}

	switch MsgType {
	case "text":
	case "bin":
	default:
		fatal("MsgType", MsgType, "is not supported.")
	}
}

func main() {
	var err error
	var laddr, raddr *net.TCPAddr
	var server *http.Server
	var conn *websocket.Conn
	var statistics_i, statistics_o int64

	if Bind != "" {
		laddr, err = net.ResolveTCPAddr("tcp", Bind)
		if err != nil {
			fatal(err)
		}
	}

	if Addr != "" {
		raddr, err = net.ResolveTCPAddr("tcp", Addr)
		if err != nil {
			fatal(err)
		}
	}

	if Listen {
		var lr net.Listener
		lr, err = net.ListenTCP("tcp", laddr)
		if err != nil {
			fatal(err)
		}

		var upgrader = &websocket.Upgrader{
			ReadBufferSize:   4 * 1024,
			WriteBufferSize:  4 * 1024,
			HandshakeTimeout: time.Second * 4,
			CheckOrigin: func(r *http.Request) bool {
				return true
			},
		}

		pimp := make(chan *websocket.Conn, 0)
		server = &http.Server{
			Handler: &requestHandler{
				Path:     Path,
				Upgrader: upgrader,
				Pimp:     pimp,
				Status:   http.StatusNotFound,
			},
			ReadHeaderTimeout: time.Second * 4,
			MaxHeaderBytes:    2048,
		}

		go server.Serve(lr)
		log("Server config done, Waiting for pimp")

		// accept a *websocket.Conn 某某某皮条客，一生只拉一条皮
		conn = <-pimp
	} else {
		var URL string

		host, port, err := net.SplitHostPort(Addr)
		if err != nil {
			fatal(err)
		}

		if ip := net.ParseIP(host); strings.ContainsRune(ip.String(), ':') { // host if ipv6
			host = "[" + host + "]"
		}

		URL = Proto + "://" + host
		if (Proto == "ws" && port != "80") || (Proto == "wss" && port != "443") {
			URL += ":" + port
		}
		URL += Path

		log("Dialing to", URL)
		dialer := &websocket.Dialer{
			NetDial: func(network, addr string) (net.Conn, error) {
				return net.DialTCP("tcp", laddr, raddr)

			},
			ReadBufferSize:   4 * 1024,
			WriteBufferSize:  4 * 1024,
			HandshakeTimeout: time.Second * 8,
		}

		conn, _, err = dialer.Dial(URL, map[string][]string{})
		if err != nil {
			fatal(err)
		}
	}

	log("Connected")

	// i
	donei := make(chan error, 0)
	go func() {
		for {
			_, msg, err := conn.ReadMessage()
			statistics_i += int64(len(msg))
			if len(msg) > 0 {
				os.Stdout.Write(msg)
			}
			if err != nil {
				donei <- err
				break
			}
		}
	}()

	// o
	doneo := make(chan error, 0)
	go func() {
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
					doneo <- ew
				case ew:
					doneo <- err
				}
				break
			}
			statistics_o += int64(ln)

		}
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
	select {
	case err = <-donei:
		log("recv-routine done:", err)
	case err = <-doneo:
		log("send-routine done:", err)
	}

	log("statistics:", statistics_i, "bytes recv,", statistics_o, "bytes sent")

	if Listen {
		log("Shuting down http server", server.Shutdown(context.Background()))
	}
}
