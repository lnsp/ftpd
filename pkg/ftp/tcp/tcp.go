package tcp

import (
	"bufio"
	"errors"
	"fmt"
	"io/ioutil"
	"net"
	"strings"
	"time"

	"github.com/lnsp/ftpd/pkg/ftp"
	"github.com/lnsp/ftpd/pkg/ftp/config"
)

// Conn is a FTP connection over TCP.
type Conn struct {
	ftp.ContextualConn
	backend     net.Conn
	reader      *bufio.Reader
	passivePort chan int
	mode        chan bool
	data        chan []byte
	status      chan error
}

// Reset resets all state within the FTP connection.
func (conn *Conn) Reset() {
	conn.mode = make(chan bool)
	conn.data = make(chan []byte)
	conn.status = make(chan error)
}

// Close closes the underlying TCP connection.
func (conn *Conn) Close() {
	conn.backend.Close()
}

// ReadCommand reads a command from the TCP connection.
func (conn *Conn) ReadCommand() (string, error) {
	buffer, _, err := conn.reader.ReadLine()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buffer)), nil
}

// Write writes raw bytes to the TCP connection.
func (conn *Conn) Write(buffer []byte) (int, error) {
	return conn.backend.Write(buffer)
}

func (conn *Conn) Receive() ([]byte, bool) {
	conn.Respond(ftp.StatusTransferReady)
	conn.mode <- true
	err := <-conn.status
	if err != nil {
		conn.Respond(ftp.StatusTransferAbort)
		return []byte{}, false
	}
	data := <-conn.data
	conn.Respond(ftp.StatusTransferDone)
	return data, true
}

func (conn *Conn) Send(data []byte) bool {
	conn.Respond(ftp.StatusTransferReady)
	conn.mode <- false
	conn.data <- data
	err := <-conn.status
	if err != nil {
		conn.Respond(ftp.StatusTransferAbort)
		return false
	}
	conn.Respond(ftp.StatusTransferDone)
	return true
}

// SetPassive passively transfers data.
// It listens on a specific port and waits for a user to connect.
func (conn *Conn) SetPassive(host string) {
	go func() {
		listener, err := net.Listen("tcp", host+":0")
		if err != nil {
			conn.status <- err
			return
		}
		defer listener.Close()
		listenerAddr := listener.Addr().(*net.TCPAddr)
		conn.passivePort <- listenerAddr.Port
		c, err := listener.Accept()
		if err != nil {
			conn.status <- err
			return
		}
		defer c.Close()

		if <-conn.mode {
			// Receive data passively
			buffer, err := ioutil.ReadAll(c)
			if err != nil {
				conn.status <- err
				return
			}
			conn.status <- nil
			conn.data <- buffer
		} else {
			// Send data passively
			_, err = c.Write(<-conn.data)
			if err != nil {
				conn.status <- err
				return
			}
			conn.status <- nil
		}
	}()
}

// SetActive actively transfers data.
// It connects to the target host and reads or writes the data from the buffer channel.
func (conn *Conn) SetActive(host string) {
	go func() {
		if <-conn.mode {
			c, err := net.Dial("tcp", host)
			if err != nil {
				conn.status <- err
				return
			}
			defer conn.Close()
			buffer, err := ioutil.ReadAll(c)
			if err != nil {
				conn.status <- err
				return
			}
			conn.status <- nil
			conn.data <- buffer
		} else {
			object := <-conn.data
			c, err := net.Dial("tcp", host)
			if err != nil {
				conn.status <- err
				return
			}
			defer c.Close()
			_, err = c.Write(object)
			if err != nil {
				conn.status <- err
				return
			}
			conn.status <- nil
		}
	}()
}

func (conn *Conn) Respond(status int, params ...interface{}) error {
	response := fmt.Sprintf("%d %s\r\n", status, fmt.Sprintf(ftp.StatusMessages[status], params...))
	_, err := conn.Write([]byte(response))
	if err != nil {
		return err
	}
	conn.Log("RESPONSE", strings.TrimSpace(response))
	return nil
}

// GetPassivePort returns the port the handler is listening on. Blocks infinitely after first time use.
func (conn *Conn) GetPassivePort() (int, error) {
	select {
	case port := <-conn.passivePort:
		return port, nil
	case <-time.After(time.Second):
		return 0, errors.New("Operation timed out")
	}
}

// NewFactory instantiates a new TCP connection factory.
func NewFactory(host string) *ConnectionFactory {
	return &ConnectionFactory{
		listener: nil,
		hostname: host,
		index:    0,
	}
}

type ConnectionFactory struct {
	listener net.Listener
	hostname string
	index    int
}

func (fac *ConnectionFactory) Listen() error {
	listener, err := net.Listen("tcp", fac.hostname)
	if err != nil {
		return err
	}
	fac.listener = listener
	return nil
}

func (fac *ConnectionFactory) Accept(cfg config.FTPUserConfig) (ftp.Conn, error) {
	c, err := fac.listener.Accept()
	if err != nil {
		return nil, err
	}
	defer func() { fac.index++ }()
	return &Conn{
		ContextualConn: ftp.ContextualConn{
			ID:           fac.index,
			Dir:          "/tmp",
			User:         "",
			TransferType: "AN",
			Config:       cfg,
		},
		backend: c,
		reader:  bufio.NewReader(c),
		mode:    make(chan bool),
		data:    make(chan []byte),
		status:  make(chan error),
	}, nil
}
