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
)

const (
	statusSyntaxError      = 500
	statusServiceReady     = 220
	statusSystemType       = 215
	statusAuthenticated    = 230
	statusNotImplemented   = 502
	statusWorkingDirectory = 257
	statusOK               = 200
	statusTransferReady    = 125
	statusTransferStart    = 150
	statusTransferDone     = 226
	statusTransferAbort    = 426
	statusPassiveMode      = 227
	statusActionError      = 250
	statusFileUnavailable  = 550
	statusFileInfo         = 213

	commandQuit             = "QUIT"
	commandUser             = "USER"
	commandPassword         = "PASS"
	commandSystemType       = "SYST"
	commandPrintDirectory   = "PWD"
	commandChangeDirectory  = "CWD"
	commandModificationTime = "MDTM"
	commandFileSize         = "SIZE"
	commandRetrieveFile     = "RETR"
	commandDataType         = "TYPE"
	commandPassiveMode      = "PASV"
	commandPort             = "PORT"
	commandListRaw          = "NLST"
	commandList             = "LIST"

	modTimeFormat       = "20060102150405"
	defaultTransferType = "AN"
)

var (
	serverPassiveBase  = flag.Int("base", 2122, "Set the passive port base")
	serverPassiveRange = flag.Int("range", 1000, "Set the passive port range")
	serverPort         = flag.Int("port", 2121, "Set the public server port")
	serverIP           = flag.String("ip", "127.0.0.1", "Set the public server IP")
	serverSystemName   = flag.String("system", "UNIX", "Set the public system name")
	statusMessages     = map[int]string{
		statusSyntaxError:      "Syntax error",
		statusServiceReady:     "Service ready",
		statusAuthenticated:    "User logged in, proceed",
		statusSystemType:       "%s Type: %s",
		statusNotImplemented:   "Command not implemented",
		statusWorkingDirectory: "\"%s\" is working directory.",
		statusOK:               "%s",
		statusTransferReady:    "Data connection already open; transfer starting",
		statusTransferStart:    "Opening data connection",
		statusTransferDone:     "Closing data connection",
		statusTransferAbort:    "Connection closed; transfer aborted",
		statusPassiveMode:      "Entering Passive Mode (%s)",
		statusFileUnavailable:  "File is unavailable",
		statusFileInfo:         "%s",
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

func handleConn(conn net.Conn) {
	defer conn.Close()
	sendResponse(conn, statusServiceReady)

	var (
		reader        = bufio.NewReader(conn)
		dataChannel   = make(chan []byte)
		statusChannel = make(chan error)
		dir           = "/"
		transferType  = defaultTransferType
	)
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			return
		}
		cmdTokens := strings.Split(strings.TrimSpace(string(line)), " ")
		if len(cmdTokens) < 1 {
			sendResponse(conn, statusSyntaxError)
			continue
		}
		cmdName := strings.ToUpper(cmdTokens[0])
		cmdData := strings.Join(cmdTokens[1:], " ")

		switch cmdName {
		case commandUser:
			sendResponse(conn, statusAuthenticated)
		case commandPassword:
			sendResponse(conn, statusAuthenticated)
		case commandSystemType:
			sendResponse(conn, statusSystemType, serverSystemName, encodeTransferType(defaultTransferType))
		case commandPrintDirectory:
			sendResponse(conn, statusWorkingDirectory, dir)
		case commandChangeDirectory:
			dir = joinPath(dir, cmdData)
			sendResponse(conn, statusWorkingDirectory, dir)
		case commandDataType:
			transferType = cmdData
			encodedType := encodeTransferType(transferType)
			if encodedType == "INVALID" {
				transferType = defaultTransferType
				sendResponse(conn, statusActionError)
				break
			}
			sendResponse(conn, statusOK, "TYPE set to "+encodedType)
		case commandModificationTime:
			info, err := os.Stat(joinPath(dir, cmdData))
			if err != nil {
				sendResponse(conn, statusFileUnavailable)
				break
			}
			sendResponse(conn, statusFileInfo, info.ModTime().Format(modTimeFormat))
		case commandFileSize:
			info, err := os.Stat(joinPath(dir, cmdData))
			if err != nil {
				sendResponse(conn, statusFileUnavailable)
				break
			}
			sendResponse(conn, statusFileInfo, strconv.FormatInt(info.Size(), 10))
		case commandRetrieveFile:
			buffer, err := ioutil.ReadFile(joinPath(dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionError)
				break
			}
			transfer(conn, buffer, dataChannel, statusChannel)
		case commandPassiveMode:
			passiveHost := *serverIP + ":" + strconv.Itoa(*serverPassiveBase+rand.Intn(*serverPassiveRange))
			dataChannel, statusChannel = transferPassive(passiveHost)
			sendResponse(conn, statusPassiveMode, generateFTPHost(passiveHost))
		case commandPort:
			dataChannel, statusChannel = transferActive(parseFTPHost(cmdData))
			sendResponse(conn, statusOK, "PORT command successfull")
		case commandListRaw:
			cmd := exec.Command("/bin/ls", "-1", dir)
			output, err := cmd.Output()
			if err != nil {
				sendResponse(conn, statusActionError)
				break
			}
			transfer(conn, encodeText(output, transferType), dataChannel, statusChannel)
		case commandList:
			cmd := exec.Command("/bin/ls", "-l", dir)
			output, err := cmd.Output()
			if err != nil {
				sendResponse(conn, statusActionError)
				break
			}
			transfer(conn, encodeText(output, transferType), dataChannel, statusChannel)
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

func sendResponse(out io.Writer, status int, params ...interface{}) error {
	response := fmt.Sprintf("%d %s\n", status, fmt.Sprintf(statusMessages[status], params...))
	_, err := io.WriteString(out, response)
	if err != nil {
		return err
	}
	log.Println("RESPONSE", strings.TrimSpace(response))
	return nil
}

func splitCommand(command string) (string, string) {
	tokens := strings.Split(command, " ")
	if len(tokens) < 1 {
		return "", ""
	}
	return tokens[0], strings.Join(tokens[1:], " ")
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

func transfer(conn net.Conn, data []byte, dataChannel chan []byte, statusChannel chan error) {
	sendResponse(conn, statusTransferReady)
	dataChannel <- data
	err := <-statusChannel
	if err != nil {
		sendResponse(conn, statusTransferAbort)
	}
	sendResponse(conn, statusTransferDone)
}

func transferPassive(host string) (chan []byte, chan error) {
	data := make(chan []byte)
	status := make(chan error)
	go func() {
		listener, err := net.Listen("tcp", host)
		if err != nil {
			status <- err
		}
		defer listener.Close()
		conn, err := listener.Accept()
		if err != nil {
			status <- err
		}
		defer conn.Close()
		_, err = conn.Write(<-data)
		if err != nil {
			status <- err
		}
		status <- nil
	}()
	return data, status
}

func transferActive(host string) (chan []byte, chan error) {
	data := make(chan []byte)
	status := make(chan error)
	go func() {
		object := <-data
		conn, err := net.Dial("tcp", host)
		if err != nil {
			status <- err
		}
		defer conn.Close()
		_, err = conn.Write(object)
		if err != nil {
			status <- err
		}
		status <- nil
	}()
	return data, status
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

func main() {
	flag.Parse()

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
		go handleConn(conn)
	}
}
