package ftp

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"net"
	"strings"

	"github.com/lnsp/ftpd/config"
)

type TCPConnection struct {
	basicFTPConnection
	Backend net.Conn
	Reader  *bufio.Reader
	Mode    chan bool
	Data    chan []byte
	Status  chan error
}

func (conn *TCPConnection) Reset() {
	conn.Mode = make(chan bool)
	conn.Data = make(chan []byte)
	conn.Status = make(chan error)
}

func (conn *TCPConnection) Close() {
	conn.Backend.Close()
}

func (conn *TCPConnection) ReadCommand() (string, error) {
	buffer, _, err := conn.Reader.ReadLine()
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(string(buffer)), nil
}

func (conn *TCPConnection) Write(buffer []byte) error {
	_, err := conn.Backend.Write(buffer)
	if err != nil {
		return err
	}
	return nil
}

func (conn *TCPConnection) Receive() ([]byte, bool) {
	conn.Respond(StatusTransferReady)
	conn.Mode <- true
	err := <-conn.Status
	if err != nil {
		conn.Respond(StatusTransferAbort)
		return []byte{}, false
	}
	data := <-conn.Data
	conn.Respond(StatusTransferDone)
	return data, true
}

func (conn *TCPConnection) Send(data []byte) bool {
	conn.Respond(StatusTransferReady)
	conn.Mode <- false
	conn.Data <- data
	err := <-conn.Status
	if err != nil {
		conn.Respond(StatusTransferAbort)
		return false
	}
	conn.Respond(StatusTransferDone)
	return true
}

// SetPassive passively transfers data.
// It listens on a specific port and waits for a user to connect.
func (peer *TCPConnection) SetPassive(host string) {
	go func() {
		listener, err := net.Listen("tcp", host)
		if err != nil {
			peer.Status <- err
			return
		}
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			peer.Status <- err
			return
		}
		defer conn.Close()

		if <-peer.Mode {
			// Receive data passively
			buffer, err := ioutil.ReadAll(conn)
			if err != nil {
				peer.Status <- err
				return
			}
			peer.Status <- nil
			peer.Data <- buffer
		} else {
			// Send data passively
			_, err = conn.Write(<-peer.Data)
			if err != nil {
				peer.Status <- err
				return
			}
			peer.Status <- nil
		}
	}()
}

// SetActive actively transfers data.
// It connects to the target host and reads or writes the data from the buffer channel.
func (peer *TCPConnection) SetActive(host string) {
	go func() {
		if <-peer.Mode {
			conn, err := net.Dial("tcp", host)
			if err != nil {
				peer.Status <- err
				return
			}
			defer conn.Close()
			buffer, err := ioutil.ReadAll(conn)
			if err != nil {
				peer.Status <- err
				return
			}
			peer.Status <- nil
			peer.Data <- buffer
		} else {
			object := <-peer.Data
			conn, err := net.Dial("tcp", host)
			if err != nil {
				peer.Status <- err
				return
			}
			defer conn.Close()
			_, err = conn.Write(object)
			if err != nil {
				peer.Status <- err
				return
			}
			peer.Status <- nil
		}
	}()
}

func (conn *TCPConnection) Respond(status int, params ...interface{}) error {
	response := fmt.Sprintf("%d %s\r\n", status, fmt.Sprintf(StatusMessages[status], params...))
	err := conn.Write([]byte(response))
	if err != nil {
		return err
	}
	conn.Log("RESPONSE", strings.TrimSpace(response))
	return nil
}
func NewTCPConnectionFactory(host string) FTPConnectionFactory {
	return &TCPConnectionFactory{
		listener: nil,
		hostname: host,
		index:    0,
	}
}

type TCPConnectionFactory struct {
	listener net.Listener
	hostname string
	index    int
}

func (fac *TCPConnectionFactory) Listen() error {
	listener, err := net.Listen("tcp", fac.hostname)
	if err != nil {
		return err
	}
	fac.listener = listener
	return nil
}

func (fac *TCPConnectionFactory) Accept(cfg config.FTPUserConfig) (FTPConnection, error) {
	conn, err := fac.listener.Accept()
	if err != nil {
		return nil, err
	}
	defer func() { fac.index++ }()
	return &TCPConnection{
		basicFTPConnection: basicFTPConnection{
			ID:           fac.index,
			Dir:          "/tmp",
			User:         "",
			TransferType: "AN",
			Config:       cfg,
		},
		Backend: conn,
		Reader:  bufio.NewReader(conn),
		Mode:    make(chan bool),
		Data:    make(chan []byte),
		Status:  make(chan error),
	}, nil
}
