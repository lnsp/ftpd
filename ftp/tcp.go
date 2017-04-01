package ftp

import (
	"bufio"
	"fmt"
	"io/ioutil"
	"log"
	"net"
	"path/filepath"
	"strings"

	"github.com/lnsp/ftpd/config"
)

type TCPConnection struct {
	Backend      net.Conn
	Reader       *bufio.Reader
	ID           int
	Mode         chan bool
	Data         chan []byte
	Status       chan error
	Dir          string
	User         string
	TransferType string
	Config       config.FTPUserConfig
}

func (conn *TCPConnection) Reset() {
	conn.Mode = make(chan bool)
	conn.Data = make(chan []byte)
	conn.Status = make(chan error)
}

func (conn *TCPConnection) GetID() int {
	return conn.ID
}

func (conn *TCPConnection) GetDir() string {
	return conn.Dir
}

func (conn *TCPConnection) GetUser() string {
	return conn.User
}

func (conn *TCPConnection) ChangeUser(to string) {
	conn.User = to
}

func (conn *TCPConnection) ChangeDir(dir string) bool {
	conn.Dir = dir
	return true
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

func (conn *TCPConnection) GetTransferType() string {
	return conn.TransferType
}

func (conn *TCPConnection) ChangeTransferType(tt string) {
	conn.TransferType = tt
}

func (conn *TCPConnection) Log(params ...interface{}) {
	log.Printf("[#%d] %s", conn.ID, fmt.Sprintln(params...))
}

func (conn *TCPConnection) GetRelativePath(p2 string) (string, bool) {
	p1 := conn.Dir
	if filepath.IsAbs(p2) {
		p1 = p2
	} else {
		p1 = filepath.Join(p1, p2)
	}
	p1, _ = filepath.Abs(p1)
	rel, err := filepath.Rel(conn.Config.FindUser(conn.User).HomeDir(), p1)
	if err != nil {
		return conn.Dir, false
	}
	if strings.Contains(rel, "..") {
		return conn.Dir, false
	}
	return p1, true
}

func (conn *TCPConnection) Receive() ([]byte, bool) {
	SendResponse(conn, StatusTransferReady)
	conn.Mode <- true
	err := <-conn.Status
	if err != nil {
		SendResponse(conn, StatusTransferAbort)
		return []byte{}, false
	}
	data := <-conn.Data
	SendResponse(conn, StatusTransferDone)
	return data, true
}

func (conn *TCPConnection) Send(data []byte) bool {
	SendResponse(conn, StatusTransferReady)
	conn.Mode <- false
	conn.Data <- data
	err := <-conn.Status
	if err != nil {
		SendResponse(conn, StatusTransferAbort)
		return false
	}
	SendResponse(conn, StatusTransferDone)
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
		Backend:      conn,
		Reader:       bufio.NewReader(conn),
		ID:           fac.index,
		Mode:         make(chan bool),
		Data:         make(chan []byte),
		Status:       make(chan error),
		Dir:          "/tmp",
		User:         "",
		TransferType: "AN",
		Config:       cfg,
	}, nil
}
