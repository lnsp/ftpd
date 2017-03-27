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
		rawRequest, _, err := reader.ReadLine()
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

		log.Println("REQUEST", cmdName, cmdData)

		switch cmdName {
		case commandUser:
			sendResponse(conn, statusAuthenticated)
		case commandPassword:
			sendResponse(conn, statusAuthenticated)
		case commandSystemType:
			sendResponse(conn, statusSystemType, *serverSystemName, encodeTransferType(defaultTransferType))
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
				sendResponse(conn, statusSyntaxParamError)
				break
			}
			sendResponse(conn, statusOK, "TYPE set to "+encodedType)
		case commandModificationTime:
			info, err := os.Stat(joinPath(dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionNotTaken)
				break
			}
			sendResponse(conn, statusFileInfo, info.ModTime().Format(modTimeFormat))
		case commandFileSize:
			info, err := os.Stat(joinPath(dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionNotTaken)
				break
			}
			sendResponse(conn, statusFileInfo, strconv.FormatInt(info.Size(), 10))
		case commandRetrieveFile:
			buffer, err := ioutil.ReadFile(joinPath(dir, cmdData))
			if err != nil {
				sendResponse(conn, statusActionNotTaken)
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
				sendResponse(conn, statusLocalError)
				break
			}
			transfer(conn, encodeText(output, transferType), dataChannel, statusChannel)
		case commandList:
			var buffer []byte
			if *enableEPLF {
				output, err := buildEPLFListing(dir)
				if err != nil {
					sendResponse(conn, statusLocalError)
					break
				}
				buffer = output
			} else {
				cmd := exec.Command("/bin/ls", "-l", dir)
				output, err := cmd.Output()
				if err != nil {
					sendResponse(conn, statusLocalError)
					break
				}
				buffer = encodeText(output, transferType)
			}
			transfer(conn, buffer, dataChannel, statusChannel)
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
	response := fmt.Sprintf("%d %s\r\n", status, fmt.Sprintf(statusMessages[status], params...))
	_, err := io.WriteString(out, response)
	if err != nil {
		return err
	}
	log.Println("RESPONSE", strings.TrimSpace(response))
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
