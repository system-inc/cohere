package flagged

// main.go is only an entry point's name in package main. Here it is a file like any other.
type Server struct { // want "`Server` is the one type .* Name it `server.go`.$"
	port int
}

func (server *Server) Port() int {
	return server.port
}
