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
	commandQuit            = "QUIT"
	commandUser            = "USER"
	commandPassword        = "PASS"
	commandSystemType      = "SYST"
	commandPrintDirectory  = "PWD"
	commandChangeDirectory = "CWD"
	commandRetrieveFile    = "RETR"
	commandDataType        = "TYPE"
	commandPassiveMode     = "PASV"
	commandPort            = "PORT"
	commandListRaw         = "NLST"
	commandList            = "LIST"

	defaultPassiveBase  = 2122
	defaultPassiveRange = 100
)

var (
	serverPort   = flag.String("port", "2121", "Sets the public server port")
	serverIP     = flag.String("ip", "127.0.0.1", "Sets the public server IP")
	descriptions = map[int]string{
		statusSyntaxError:      "Syntax error",
		statusServiceReady:     "Service ready",
		statusAuthenticated:    "User logged in, proceed",
		statusSystemType:       "%s system",
		statusNotImplemented:   "Command not implemented",
		statusWorkingDirectory: "\"%s\" is working directory.",
		statusOK:               "%s",
		statusTransferReady:    "Data connection already open; transfer starting",
		statusTransferStart:    "Opening data connection",
		statusTransferDone:     "Closing data connection",
		statusTransferAbort:    "Connection closed; transfer aborted",
		statusPassiveMode:      "Entering Passive Mode (%s)",
	}
)

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

func encodeASCII(ascii string) []byte {
	return []byte(strings.Replace(ascii, "\n", "\r\n", -1))
}

func buildResponse(status int, params ...interface{}) string {
	resp := fmt.Sprintf(descriptions[status], params...)
	return fmt.Sprintf("%d %s\n", status, resp)
}

func sendResponse(out io.Writer, status int, params ...interface{}) error {
	response := buildResponse(status, params...)
	_, err := io.WriteString(out, response)
	if err != nil {
		return err
	}
	fmt.Println("RESPONSE", strings.TrimSpace(response))
	return nil
}

func splitCommand(command string) (string, string) {
	tokens := strings.Split(command, " ")
	if len(tokens) < 1 {
		return "", ""
	}
	return tokens[0], strings.Join(tokens[1:], " ")
}

func buildDirectoryListing(dir, flags string) string {
	cmd := exec.Command("/bin/ls", "-l", dir)
	output, err := cmd.Output()
	if err != nil {
		return err.Error() + "\n"
	}
	return string(output)
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

func handleConn(conn net.Conn) {
	defer conn.Close()

	io.WriteString(conn, buildResponse(statusServiceReady))
	var dataChannel chan []byte
	var statusChannel chan error

	dir := "/"
	reader := bufio.NewReader(conn)
	for {
		line, _, err := reader.ReadLine()
		if err != nil {
			return
		}
		command := strings.TrimSpace(string(line))
		fmt.Println("REQUEST", command)
		name, data := splitCommand(command)
		switch name {
		case commandUser:
			sendResponse(conn, statusAuthenticated)
		case commandPassword:
			sendResponse(conn, statusAuthenticated)
		case commandSystemType:
			sendResponse(conn, statusSystemType, "UNIX")
		case commandPrintDirectory:
			sendResponse(conn, statusWorkingDirectory, dir)
		case commandChangeDirectory:
			if strings.HasPrefix(data, "/") {
				dir = data
			} else {
				dir = filepath.Join(dir, data)
			}
			sendResponse(conn, statusWorkingDirectory, dir)
		case commandDataType:
			sendResponse(conn, statusOK, "Type set to "+data)
		case commandRetrieveFile:
			var filename string
			if strings.HasPrefix(data, "/") {
				filename = data
			} else {
				filename = filepath.Join(dir, data)
			}
			data, err := ioutil.ReadFile(filename)
			if err != nil {
				sendResponse(conn, statusActionError)
				break
			}
			transfer(conn, data, dataChannel, statusChannel)
		case commandPassiveMode:
			passiveHost := *serverIP + ":" + strconv.Itoa(defaultPassiveBase+rand.Intn(defaultPassiveRange))
			dataChannel, statusChannel = transferPassive(passiveHost)
			sendResponse(conn, statusPassiveMode, generateFTPHost(passiveHost))
		case commandPort:
			dataChannel, statusChannel = transferActive(parseFTPHost(data))
			sendResponse(conn, statusOK, "PORT command successfull")
		case commandListRaw:
			cmd := exec.Command("/bin/ls", "-1", dir)
			output, err := cmd.Output()
			if err != nil {
				sendResponse(conn, statusActionError)
				break
			}
			data := encodeASCII(string(output))
			transfer(conn, data, dataChannel, statusChannel)
		case commandList:
			cmd := exec.Command("/bin/ls", "-l", dir)
			output, err := cmd.Output()
			if err != nil {
				sendResponse(conn, statusActionError)
				break
			}
			data := encodeASCII(string(output))
			transfer(conn, data, dataChannel, statusChannel)
		case commandQuit:
			sendResponse(conn, statusOK, "Connection closing")
			return
		default:
			sendResponse(conn, statusNotImplemented)
		}
	}
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

func main() {
	flag.Parse()

	listener, err := net.Listen("tcp", *serverIP+":"+*serverPort)
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
