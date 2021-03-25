package main

import "net"
import "net/http"

import "github.com/gorilla/websocket"

type requestHandler struct {
	Addr     *net.TCPAddr
	Path     string
	Upgrader *websocket.Upgrader
	Pimp     chan *websocket.Conn
	Status   int "伪装状态"
	done     bool
}

func (h *requestHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	log(request.RemoteAddr, "access to", request.RequestURI)

	if h.done { // 服务停止
		writer.WriteHeader(h.Status)
		return
	}

	if h.Addr != nil && h.Addr.String() != request.RemoteAddr { // 未知客户端
		writer.WriteHeader(h.Status)
		return
	}

	if request.URL.Path != h.Path { // 未知路径
		writer.WriteHeader(http.StatusNotFound)
		return
	}

	if request.Header.Get("upgrade") != "websocket" { // 非websocket连接请求
		writer.WriteHeader(h.Status)
		return
	}

	conn, err := h.Upgrader.Upgrade(writer, request, nil)
	if err != nil {
		fatal(err)
		return
	}

	/*
		forwardedAddrs := http_proto.ParseXForwardedFor(request.Header)
		remoteAddr := conn.RemoteAddr()
		if len(forwardedAddrs) > 0 && forwardedAddrs[0].Family().IsIP() {
			remoteAddr = &net.TCPAddr{
				IP:   forwardedAddrs[0].IP(),
				Port: int(0),
			}
		}*/

	h.Pimp <- conn
	h.done = true
}
