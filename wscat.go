package main

import "context"
import "crypto/tls"
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
var Lowkey bool
var Cert string
var Key string
var Insec bool
var Proto string
var Bind string
var Peer string
var Path string
var MsgType string

var Host, Port string

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
	var fuck []interface{}
	var e = len(v) - 1

	for i, f := range v {
		fuck = append(fuck, f)
		if i < e {
			fuck = append(fuck, " ")
		}
	}

	damn.Fatal(fuck...)
}

func init() {
	flag.BoolVar(&Verbose, "v", false, "verbose output")
	flag.BoolVar(&Listen, "listen", false, "run as websocket server")
	flag.BoolVar(&Lowkey, "lowkey", false, "similar to listen, but server shuts down when it accepted a client")
	flag.StringVar(&Cert, "cert", "", "wss server tls cert file")
	flag.StringVar(&Key, "key", "", "wss server tls key file")
	flag.BoolVar(&Insec, "insec", false, "client skip verifies the server's cert")
	flag.StringVar(&Proto, "proto", "ws", "ws(plain text) or wss(ws over tls)")
	flag.StringVar(&Bind, "bind", "", "local addr")
	flag.StringVar(&Peer, "peer", "", "peer addr to connect to or limit client addr while as a server")
	flag.StringVar(&Path, "path", "/", "websocket url path")
	flag.StringVar(&MsgType, "type", "text", "websocket message type")
	flag.Parse()

	switch Proto {
	case "ws":
	case "wss":
	default:
		fatal("Proto", Proto, "is not supported")
	}

	if Path[0] != '/' {
		fatal("Path must start with `/'")
	}

	if !Listen && !Lowkey && Peer == "" {
		fatal("Must specify addr to connect to")
	}

	if Listen && Lowkey {
		fatal("Accept one of \"-listen/-lowkey\" only")
	}

	if (Listen || Lowkey) && Proto == "wss" && (Cert == "" || Key == "") {
		fatal("Must specify tls cert and key while run as a wss server")
	}

	if (Listen || Lowkey) && Insec {
		fatal("Option \"-insec\" incompatible with server mode")
	}

	switch MsgType {
	case "text":
	case "bin":
	default:
		fatal("MsgType", MsgType, "is not supported")
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

	if Peer != "" {
		raddr, err = net.ResolveTCPAddr("tcp", Peer)
		if err != nil {
			fatal(err)
		}
	}

	if Listen || Lowkey {
		var lr net.Listener

		if Proto == "ws" {
			lr, err = net.ListenTCP("tcp", laddr)
			if err != nil {
				fatal(err)
			}
		} else if Proto == "wss" {
			var cert tls.Certificate
			cert, err = tls.LoadX509KeyPair(Cert, Key)
			if err != nil {
				fatal(err)
			}

			lr, err = tls.Listen("tcp", laddr.String(), &tls.Config{Certificates: []tls.Certificate{cert}})
			if err != nil {
				fatal(err)
			}
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
				Addr:     raddr,
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

		Host, Port, err := net.SplitHostPort(Peer)
		if err != nil {
			fatal(err)
		}

		if ip := net.ParseIP(Host); strings.ContainsRune(ip.String(), ':') { // host if ipv6
			Host = "[" + Host + "]"
		}

		URL = Proto + "://" + Host
		if (Proto == "ws" && Port != "80") || (Proto == "wss" && Port != "443") {
			URL += ":" + Port
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

		if Proto == "wss" {
			dialer.TLSClientConfig = &tls.Config{}
			if Insec {
				dialer.TLSClientConfig.InsecureSkipVerify = true
			} else {
				dialer.TLSClientConfig.ServerName = Host
				dialer.TLSClientConfig.InsecureSkipVerify = false
			}

		}

		conn, _, err = dialer.Dial(URL, map[string][]string{})
		if err != nil {
			fatal(err)
		}
	}

	log("Connected")

	if Lowkey {
		log("Shutting down http server early as it is running low-key", server.Shutdown(context.Background()))
	}

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
		log("Shutting down http server", server.Shutdown(context.Background()))
	}
}
