package allowed

// A type of several words is named in snake_case.
type HttpServer struct {
	port int
}

func (server *HttpServer) Port() int {
	return server.port
}
