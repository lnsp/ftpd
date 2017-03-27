// Copyright 2017 Lennart Espe <lennart@espe.tech>
// All rights reserved.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"io"
	"io/ioutil"
	"log"
	"math/rand"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

const (
	statusRestartMarker   = 110
	statusServiceNotReady = 120
	statusTransferReady   = 125
	statusTransferStart   = 150

	statusOK               = 200
	statusNotImplementedOK = 202
	statusSystemInfo       = 211
	statusDirectoryInfo    = 212
	statusFileInfo         = 213
	statusHelpInfo         = 214
	statusSystemType       = 215
	statusServiceReady     = 220
	statusCloseConnection  = 221
	statusTransferOpen     = 225
	statusTransferDone     = 226
	statusPassiveMode      = 227
	statusAuthenticated    = 230
	statusActionDone       = 250
	statusWorkingDirectory = 257

	statusNeedPassword = 331
	statusNeedAccount  = 332
	statusNeedMoreInfo = 350

	statusServiceUnavailable = 421
	statusTransferFailed     = 425
	statusTransferAbort      = 426
	statusActionNotTaken     = 450
	statusLocalError         = 451
	statusInsufficientSpace  = 452

	statusSyntaxError            = 500
	statusSyntaxParamError       = 501
	statusNotImplemented         = 502
	statusBadSequence            = 503
	statusNotImplementedParam    = 504
	statusNotLoggedIn            = 530
	statusStorageAccountRequired = 532
	statusUnknownPage            = 551
	statusInsufficientSpaceAbort = 552
	statusInvalidName            = 553

	commandQuit             = "QUIT"
	commandUser             = "USER"
	commandPassword         = "PASS"
	commandSystemType       = "SYST"
	commandPrintDirectory   = "PWD"
	commandChangeDirectory  = "CWD"
	commandModificationTime = "MDTM"
	commandFileSize         = "SIZE"
	commandStoreFile        = "STOR"
	commandRetrieveFile     = "RETR"
	commandDataType         = "TYPE"
	commandPassiveMode      = "PASV"
	commandPort             = "PORT"
	commandListRaw          = "NLST"
	commandList             = "LIST"

	modTimeFormat       = "20060102150405"
	defaultTransferType = "AN"
	transferBufferSize  = 4096
)

var (
	enableEPLF         = flag.Bool("eplf", false, "Enable EPLF (Easy parsed LIST Format)")
	serverPassiveBase  = flag.Int("base", 2122, "Set the passive port base")
	serverPassiveRange = flag.Int("range", 1000, "Set the passive port range")
	serverPort         = flag.Int("port", 2121, "Set the public server port")
	serverIP           = flag.String("ip", "127.0.0.1", "Set the public server IP")
	serverSystemName   = flag.String("system", "UNIX", "Set the public system name")
	statusMessages     = map[int]string{
		statusRestartMarker:   "Restart marker reply",
		statusServiceNotReady: "Service ready in %d minutes",
		statusTransferReady:   "Data connection already open; transfer starting",
		statusTransferStart:   "Opening data connection",

		statusOK:               "%s",
		statusNotImplementedOK: "Command not implemented",
		statusSystemInfo:       "%s",
		statusDirectoryInfo:    "%s",
		statusFileInfo:         "%s",
		statusHelpInfo:         "%s",
		statusSystemType:       "%s Type: %s",
		statusServiceReady:     "FTP Service ready",
		statusCloseConnection:  "Service closing control connection",
		statusTransferOpen:     "Data connection open; no transfer in progress",
		statusTransferDone:     "Closing data connection",
		statusPassiveMode:      "Entering Passive Mode (%s)",
		statusAuthenticated:    "User logged in, proceed",
		statusActionDone:       "Requested file action okay, completed",
		statusWorkingDirectory: "\"%s\" is working directory.",

		statusNeedPassword: "User name okay, need password",
		statusNeedAccount:  "Need account for login",
		statusNeedMoreInfo: "Requested file action pending further information",

		statusServiceUnavailable: "Service not available, closing control connection",
		statusTransferFailed:     "Can't open data connection",
		statusTransferAbort:      "Connection closed; transfer aborted",
		statusActionNotTaken:     "Requested file action not taken; file unavailable",
		statusLocalError:         "Reqzested action aborted; local error in processing",
		statusInsufficientSpace:  "Requested action not taken; insufficient storage space in system",

		statusSyntaxError:            "Syntax error",
		statusSyntaxParamError:       "Syntax error in parameters or arguments",
		statusNotImplemented:         "Command not implemented",
		statusBadSequence:            "Bad sequence of commands",
		statusNotImplementedParam:    "Command not implemented for that parameter",
		statusNotLoggedIn:            "Not logged in",
		statusStorageAccountRequired: "Need account for storing files",
		statusUnknownPage:            "Requested action aborted; page type unknown",
		statusInvalidName:            "Requested action not taken; file name not allowed",
	}
	transferTypes = map[rune]string{
		'A': "ASCII",
		'E': "EBCDIC",
		'I': "BINARY",
		'L': "LOCAL FORMAT",
		'N': "NON PRINT",
		'T': "TELNET",
		'C': "ASA CARRIAGE CONTROL",
	}
)

type ConnectionFactory struct {
	index int
}

func (fac *ConnectionFactory) NewHandler(conn net.Conn) *Connection {
	defer func() { fac.index++ }()
	return &Connection{
		conn,
		fac.index,
		bufio.NewReader(conn),
		make(chan bool),
		make(chan []byte),
		make(chan error),
		"/",
		"anonymous",
		defaultTransferType,
	}
}

type Connection struct {
	net.Conn
	ID           int
	Reader       *bufio.Reader
	Mode         chan bool
	Data         chan []byte
	Status       chan error
	Dir          string
	User         string
	TransferType string
}

func (conn *Connection) log(params ...interface{}) {
	log.Printf("[#%d] %s", conn.ID, fmt.Sprintln(params...))
}

func (conn *Connection) Handle() {
	defer conn.Close()
	sendResponse(conn, statusServiceReady)
	for {
		rawRequest, _, err := conn.Reader.ReadLine()
		if err != nil {
			return
		}
		cmdTokens := strings.Split(strings.TrimSpace(string(rawRequest)), " ")
		if len(cmdTokens) < 1 {
			sendResponse(conn, statusSyntaxError)
			continue
		}
		cmdName := strings.ToUpper(cmdTokens[0])
		cmdData := strings.Join(cmdTokens[1:], " ")

		conn.log("REQUEST", cmdName, cmdData)

		switch cmdName {
		case commandUser:
			sendResponse(conn, statusAuthenticated)
		case commandPassword:
			sendResponse(conn, statusAuthenticated)
		case commandSystemType:
			sendResponse(conn, statusSystemType, *serverSystemName, encodeTransferType(defaultTransferType))
		case commandPrintDirectory:
			sendResponse(conn, statusWorkingDirectory, conn.Dir)
		case commandChangeDirectory:
			conn.Dir = joinPath(conn.Dir, cmdData)
			sendResponse(conn, statusWorkingDirectory, conn.Dir)
		case commandDataType:
			encodedType := encodeTransferType(cmdData)
			if encodedType == "INVALID" {
				sendResponse(conn, statusSyntaxParamError)
				break
			}
			conn.TransferType = cmdData
			sendResponse(conn, statusOK, "TYPE set to "+encodedType)
		case commandModificationTime:
			info, err := os.Stat(joinPath(conn.Dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionNotTaken)
				break
			}
			sendResponse(conn, statusFileInfo, info.ModTime().Format(modTimeFormat))
		case commandFileSize:
			info, err := os.Stat(joinPath(conn.Dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionNotTaken)
				break
			}
			sendResponse(conn, statusFileInfo, strconv.FormatInt(info.Size(), 10))
		case commandRetrieveFile:
			buffer, err := ioutil.ReadFile(joinPath(conn.Dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionNotTaken)
				break
			}
			sendTo(conn, buffer)
		case commandStoreFile:
			path := joinPath(conn.Dir, cmdData)
			data, success := receiveFrom(conn)
			if !success {
				break
			}
			if err := ioutil.WriteFile(path, data, 0644); err != nil {
				sendResponse(conn, statusActionNotTaken)
				break
			}
		case commandPassiveMode:
			passiveHost := *serverIP + ":" + strconv.Itoa(*serverPassiveBase+rand.Intn(*serverPassiveRange))
			transferPassive(passiveHost, conn)
			sendResponse(conn, statusPassiveMode, generateFTPHost(passiveHost))
		case commandPort:
			transferActive(parseFTPHost(cmdData), conn)
			sendResponse(conn, statusOK, "PORT command successfull")
		case commandListRaw:
			cmd := exec.Command("/bin/ls", "-1", conn.Dir)
			output, err := cmd.Output()
			if err != nil {
				sendResponse(conn, statusLocalError)
				break
			}
			sendTo(conn, encodeText(output, conn.TransferType))
		case commandList:
			var buffer []byte
			if *enableEPLF {
				output, err := buildEPLFListing(conn.Dir)
				if err != nil {
					sendResponse(conn, statusLocalError)
					break
				}
				buffer = output
			} else {
				cmd := exec.Command("/bin/ls", "-l", conn.Dir)
				output, err := cmd.Output()
				if err != nil {
					sendResponse(conn, statusLocalError)
					break
				}
				buffer = encodeText(output, conn.TransferType)
			}
			sendTo(conn, buffer)
		case commandQuit:
			sendResponse(conn, statusOK, "Connection closing")
			return
		default:
			sendResponse(conn, statusNotImplemented)
		}
	}
}

func encodeText(text []byte, mode string) []byte {
	return []byte(strings.Replace(string(text), "\n", "\r\n", -1))
}

func buildResponse(status int, params ...interface{}) string {
	resp := fmt.Sprintf(statusMessages[status], params...)
	return fmt.Sprintf("%d %s\n", status, resp)
}

func sendResponse(conn *Connection, status int, params ...interface{}) error {
	response := fmt.Sprintf("%d %s\r\n", status, fmt.Sprintf(statusMessages[status], params...))
	_, err := io.WriteString(conn, response)
	if err != nil {
		return err
	}
	conn.log("RESPONSE", strings.TrimSpace(response))
	return nil
}

func joinPath(p1, p2 string) string {
	if filepath.IsAbs(p2) {
		p1 = p2
	} else {
		p1 = filepath.Join(p1, p2)
	}
	p1, _ = filepath.Abs(p1)
	return p1
}

func receiveFrom(conn *Connection) ([]byte, bool) {
	sendResponse(conn, statusTransferReady)
	conn.Mode <- true
	err := <-conn.Status
	if err != nil {
		sendResponse(conn, statusTransferAbort)
		return []byte{}, false
	}
	data := <-conn.Data
	sendResponse(conn, statusTransferDone)
	return data, true
}

func sendTo(conn *Connection, data []byte) bool {
	sendResponse(conn, statusTransferReady)
	conn.Mode <- false
	conn.Data <- data
	err := <-conn.Status
	if err != nil {
		sendResponse(conn, statusTransferAbort)
		return false
	}
	sendResponse(conn, statusTransferDone)
	return true
}

// transferPassive passively transfers data.
// It listens on a specific port and waits for a user to connect.
func transferPassive(host string, peer *Connection) {
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

// transferActive actively transfers data.
// It connects to the target host and reads or writes the data from the buffer channel.
func transferActive(host string, peer *Connection) {
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

func parseFTPHost(ports string) string {
	tokens := strings.Split(ports, ",")
	host := strings.Join(tokens[:4], ".")
	base1, _ := strconv.Atoi(tokens[4])
	base0, _ := strconv.Atoi(tokens[5])
	port := strconv.Itoa(base1*256 + base0)
	return host + ":" + port
}

func generateFTPHost(hostport string) string {
	tokens := strings.Split(hostport, ":")
	ips := strings.Split(tokens[0], ".")
	port, _ := strconv.Atoi(tokens[1])
	return fmt.Sprintf("%s,%d,%d", strings.Join(ips, ","), port/256, port%256)
}

func encodeTransferType(tt string) string {
	var (
		baseMode, extMode string
		found             bool
		modeRunes         = []rune(tt)
	)
	if baseMode, found = transferTypes[modeRunes[0]]; !found {
		return "INVALID"
	}
	if len(modeRunes) == 2 {
		if extMode, found = transferTypes[modeRunes[1]]; !found {
			return "INVALID"
		}
	} else {
		extMode = transferTypes['N']
	}
	return fmt.Sprintf("%s %s", baseMode, extMode)
}

func buildEPLFListing(dir string) ([]byte, error) {
	// This IS DIRTY. Does not work on Windows.
	output := ""
	directory, err := ioutil.ReadDir(dir)
	if err != nil {
		return []byte{}, err
	}
	for _, info := range directory {
		if !info.Mode().IsDir() && !info.Mode().IsRegular() {
			continue
		}
		output += "+"
		var stat syscall.Stat_t
		err = syscall.Stat(filepath.Join(dir, info.Name()), &stat)
		if err != nil {
			return []byte{}, err
		}
		output += "i" + strconv.FormatInt(int64(stat.Dev), 10) + "." + strconv.FormatUint(stat.Ino, 10) + ","
		output += "m" + strconv.FormatInt(info.ModTime().Unix(), 10) + ","
		if info.Mode().IsRegular() {
			output += "s" + strconv.FormatInt(info.Size(), 10) + ",r,"
		} else {
			output += "/,"
		}
		output += "\x09" + info.Name() + "\x0d\x0a"
	}
	return []byte(output), nil
}

func main() {
	flag.Parse()
	factory := ConnectionFactory{}
	listener, err := net.Listen("tcp", *serverIP+":"+strconv.Itoa(*serverPort))
	if err != nil {
		log.Fatal(err)
	}
	for {
		conn, err := listener.Accept()
		if err != nil {
			log.Print(err)
			continue
		}
		handler := factory.NewHandler(conn)
		go handler.Handle()
	}
}
