package tcp

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	"github.com/lnsp/ftpd/config"
	"github.com/lnsp/ftpd/ftp"
)

type conn struct {
	ftp.ContextualConn
	Backend net.Conn
	Reader  *bufio.Reader
	Mode    chan bool
	Data    chan []byte
	Status  chan error
}

func (conn *conn) Reset() {
	conn.Mode = make(chan bool)
	conn.Data = make(chan []byte)
	conn.Status = make(chan error)
}

func (conn *conn) Close() {
	conn.Backend.Close()
}

func (conn *conn) ReadCommand() (string, error) {
	buffer, _, err := conn.Reader.ReadLine()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buffer)), nil
}

func (conn *conn) Write(buffer []byte) error {
	_, err := conn.Backend.Write(buffer)
	if err != nil {
		return err
	}
	return nil
}

func (conn *conn) Receive() ([]byte, bool) {
	conn.Respond(ftp.StatusTransferReady)
	conn.Mode <- true
	err := <-conn.Status
	if err != nil {
		conn.Respond(ftp.StatusTransferAbort)
		return []byte{}, false
	}
	data := <-conn.Data
	conn.Respond(ftp.StatusTransferDone)
	return data, true
}

func (conn *conn) Send(data []byte) bool {
	conn.Respond(ftp.StatusTransferReady)
	conn.Mode <- false
	conn.Data <- data
	err := <-conn.Status
	if err != nil {
		conn.Respond(ftp.StatusTransferAbort)
		return false
	}
	conn.Respond(ftp.StatusTransferDone)
	return true
}

// SetPassive passively transfers data.
// It listens on a specific port and waits for a user to connect.
func (conn *conn) SetPassive(host string) {
	go func() {
		listener, err := net.Listen("tcp", host)
		if err != nil {
			conn.Status <- err
			return
		}
		defer listener.Close()
		c, err := listener.Accept()
		if err != nil {
			conn.Status <- err
			return
		}
		defer c.Close()

		if <-conn.Mode {
			// Receive data passively
			buffer, err := ioutil.ReadAll(c)
			if err != nil {
				conn.Status <- err
				return
			}
			conn.Status <- nil
			conn.Data <- buffer
		} else {
			// Send data passively
			_, err = c.Write(<-conn.Data)
			if err != nil {
				conn.Status <- err
				return
			}
			conn.Status <- nil
		}
	}()
}

// SetActive actively transfers data.
// It connects to the target host and reads or writes the data from the buffer channel.
func (conn *conn) SetActive(host string) {
	go func() {
		if <-conn.Mode {
			c, err := net.Dial("tcp", host)
			if err != nil {
				conn.Status <- err
				return
			}
			defer conn.Close()
			buffer, err := ioutil.ReadAll(c)
			if err != nil {
				conn.Status <- err
				return
			}
			conn.Status <- nil
			conn.Data <- buffer
		} else {
			object := <-conn.Data
			c, err := net.Dial("tcp", host)
			if err != nil {
				conn.Status <- err
				return
			}
			defer c.Close()
			_, err = c.Write(object)
			if err != nil {
				conn.Status <- err
				return
			}
			conn.Status <- nil
		}
	}()
}

func (conn *conn) Respond(status int, params ...interface{}) error {
	response := fmt.Sprintf("%d %s\r\n", status, fmt.Sprintf(ftp.StatusMessages[status], params...))
	err := conn.Write([]byte(response))
	if err != nil {
		return err
	}
	conn.Log("RESPONSE", strings.TrimSpace(response))
	return nil
}

// NewFactory instantiates a new TCP connection factory.
func NewFactory(host string) ftp.ConnectionFactory {
	return &connectionFactory{
		listener: nil,
		hostname: host,
		index:    0,
	}
}

type connectionFactory struct {
	listener net.Listener
	hostname string
	index    int
}

func (fac *connectionFactory) Listen() error {
	listener, err := net.Listen("tcp", fac.hostname)
	if err != nil {
		return err
	}
	fac.listener = listener
	return nil
}

func (fac *connectionFactory) Accept(cfg config.FTPUserConfig) (ftp.Conn, error) {
	c, err := fac.listener.Accept()
	if err != nil {
		return nil, err
	}
	defer func() { fac.index++ }()
	return &conn{
		ContextualConn: ftp.ContextualConn{
			ID:           fac.index,
			Dir:          "/tmp",
			User:         "",
			TransferType: "AN",
			Config:       cfg,
		},
		Backend: c,
		Reader:  bufio.NewReader(c),
		Mode:    make(chan bool),
		Data:    make(chan []byte),
		Status:  make(chan error),
	}, nil
}
